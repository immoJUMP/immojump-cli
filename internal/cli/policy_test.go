package cli

import (
	"strings"
	"testing"
)

func TestReadonlyBlocksEverythingButRead(t *testing.T) {
	blocked := [][]string{
		{"--readonly", "contacts", "create", "--set", "first_name=Ada"}, // write
		{"--readonly", "contacts", "delete", "42"},                      // destructive
		{"--readonly", "shares", "create", "--immobilie", "5"},          // external
		{"--readonly", "shares", "revoke", "7"},                         // write
		// publish stellt die Datei unbefristet und ohne Anmeldung ins Netz —
		// haerter als ein Freigabe-Link, der ablaeuft und widerrufbar ist.
		{"--readonly", "documents", "publish", "11"}, // external
	}
	for _, args := range blocked {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(args...)
			if code != 6 {
				t.Fatalf("Exit 6 erwartet, got %d (stderr: %s)", code, stderr)
			}
			if h.last != nil {
				t.Error("blockierter Befehl darf keinen Request absetzen")
			}
			line := errorLine(t, stderr)
			if line["code"] != "POLICY_BLOCKED" {
				t.Errorf("code POLICY_BLOCKED erwartet, got %#v", line["code"])
			}
		})
	}
}

func TestReadonlyAllowsRead(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("--readonly", "contacts", "list"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last == nil {
		t.Error("Lesebefehl muss durchgehen")
	}
}

func TestAllowListLimitsRiskLevels(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("--allow", "read,write", "contacts", "create", "--set", "first_name=Ada"); code != 0 {
		t.Fatalf("write erlaubt, Exit 0 erwartet, got %d (%s)", code, stderr)
	}

	h2 := newHarness(t)
	code, _, stderr := h2.run("--allow", "read,write", "contacts", "delete", "42")
	if code != 6 {
		t.Fatalf("destructive nicht erlaubt, Exit 6 erwartet, got %d", code)
	}
	if errorLine(t, stderr)["code"] != "POLICY_BLOCKED" {
		t.Error("POLICY_BLOCKED erwartet")
	}

	h3 := newHarness(t)
	if code, _, _ := h3.run("--allow", "read,write", "shares", "create", "--immobilie", "5"); code != 6 {
		t.Fatalf("external nicht erlaubt, Exit 6 erwartet, got %d", code)
	}
}

func TestAllowEnvIsHonoured(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_ALLOW"] = "read"
	code, _, stderr := h.run("contacts", "create", "--set", "first_name=Ada")
	if code != 6 {
		t.Fatalf("IMMOJUMP_ALLOW=read soll blocken, got %d (%s)", code, stderr)
	}

	h2 := newHarness(t)
	h2.env["IMMOJUMP_ALLOW"] = "read"
	if code, _, _ := h2.run("contacts", "list"); code != 0 {
		t.Fatalf("Lesen bleibt erlaubt, got %d", code)
	}
}

func TestAllowFlagBeatsEnv(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_ALLOW"] = "read"
	if code, _, stderr := h.run("--allow", "read,write", "contacts", "create", "--set", "first_name=Ada"); code != 0 {
		t.Fatalf("--allow schlägt IMMOJUMP_ALLOW, Exit 0 erwartet, got %d (%s)", code, stderr)
	}
}

func TestReadonlyBeatsAllowFlag(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("--readonly", "--allow", "read,write,destructive", "contacts", "delete", "1"); code != 6 {
		t.Fatal("--readonly muss gewinnen")
	}
}

// TestApiEscapeHatchRiskFallsBackToMethod deckt die Pfade ab, die KEINE
// Registry-Spec haben: GET=read, DELETE=destructive, alles andere external.
func TestApiEscapeHatchRiskFallsBackToMethod(t *testing.T) {
	cases := []struct {
		method string
		policy string
		want   int
	}{
		{"GET", "--readonly", 0},
		{"POST", "--readonly", 6},
		{"DELETE", "--allow=read,write", 6},
		{"POST", "--allow=read,write", 6},               // unbekannt -> external
		{"POST", "--allow=read,write,destructive", 6},   // external fehlt weiterhin
		{"POST", "--allow=read,write,external", 0},      // erst external lässt durch
		{"delete", "--allow=read,write,destructive", 0}, // Kleinschreibung zählt
		{"PATCH", "--allow=read,write", 6},              // konservativ nach oben
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.policy, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.policy, "api", tc.method, "/api/gibtsnicht")
			if code != tc.want {
				t.Errorf("Exit %d erwartet, got %d (%s)", tc.want, code, stderr)
			}
		})
	}
}

// TestApiEscapeHatchRiskFromRegistry ist der eigentliche Bypass-Schutz: Wer
// einen kuratierten Endpoint über "api" aufruft, bekommt dessen Risk-Level —
// nicht das lasche Methoden-Mapping.
func TestApiEscapeHatchRiskFromRegistry(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		method string
		path   string
		want   int
	}{
		// shares create ist external — POST /api/share-links darf sich nicht
		// als "write" durch die Policy schmuggeln.
		{"share-links create", "--allow=read,write", "POST", "/api/share-links", 6},
		{"share-links create erlaubt", "--allow=read,write,external", "POST", "/api/share-links", 0},
		// shares update ist external (reaktiviert Links, entsperrt Passwörter).
		{"share-links update", "--allow=read,write", "PATCH", "/api/share-links/7", 6},
		// Lesen bleibt lesen — auch über den Escape-Hatch.
		{"contacts list", "--readonly", "GET", "/api/contacts", 0},
		{"contacts get", "--readonly", "GET", "/api/contacts/42", 0},
		{"pipelines list mit {org}", "--readonly", "GET", "/api/pipelines/org-test/pipelines", 0},
		// contacts delete ist destructive.
		{"contacts delete", "--allow=read,write", "DELETE", "/api/contacts/42", 6},
		{"contacts delete erlaubt", "--allow=read,write,destructive", "DELETE", "/api/contacts/42", 0},
		// contacts create ist write — kein Grund, external zu verlangen.
		{"contacts create", "--allow=read,write", "POST", "/api/contacts", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.policy, "api", tc.method, tc.path)
			if code != tc.want {
				t.Fatalf("Exit %d erwartet, got %d (%s)", tc.want, code, stderr)
			}
			if tc.want == 6 && h.last != nil {
				t.Error("blockierter Escape-Hatch darf keinen Request absetzen")
			}
		})
	}
}

// TestApiPathTemplateMatchesExactlyOneSegment: {id} darf nicht über
// Schrägstriche hinweg matchen, sonst erbt ein Unterpfad ein zu mildes Risk.
func TestApiPathTemplateMatchesExactlyOneSegment(t *testing.T) {
	// /api/contacts/{id} ist read — /api/contacts/42/mehr/tiefer nicht.
	h := newHarness(t)
	if code, _, _ := h.run("--readonly", "api", "GET", "/api/contacts/42/mehr/tiefer"); code != 0 {
		t.Fatalf("GET fällt auf read zurück, Exit 0 erwartet, got %d", code)
	}
	h2 := newHarness(t)
	if code, _, _ := h2.run("--allow=read,write", "api", "POST", "/api/contacts/42/mehr/tiefer"); code != 6 {
		t.Fatalf("unbekannter POST-Unterpfad ist external, Exit 6 erwartet, got %d", code)
	}
}

func TestUnknownRiskLevelInAllowIsConfigError(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("--allow", "read,quatsch", "contacts", "list")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("bei kaputter Policy darf kein Request rausgehen")
	}
}

// TestEmptyAllowListIsConfigError: fail-closed. Eine gesetzte, aber leere
// Liste ist ein Konfigurationsfehler — niemals "dann eben alles erlauben".
func TestEmptyAllowListIsConfigError(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
	}{
		{"--allow leer", []string{"--allow", "", "contacts", "list"}, ""},
		{"--allow nur Komma", []string{"--allow", ",", "contacts", "list"}, ""},
		{"--allow nur Whitespace", []string{"--allow", "  ", "contacts", "list"}, ""},
		{"--allow Kommas und Leerzeichen", []string{"--allow", " , , ", "contacts", "list"}, ""},
		{"IMMOJUMP_ALLOW nur Komma", []string{"contacts", "list"}, ","},
		{"IMMOJUMP_ALLOW nur Whitespace", []string{"contacts", "list"}, " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.env != "" {
				h.env["IMMOJUMP_ALLOW"] = tc.env
			}
			code, _, stderr := h.run(tc.args...)
			if code != 3 {
				t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("bei leerer Policy darf kein Request rausgehen")
			}
			msg, _ := errorLine(t, stderr)["message"].(string)
			if !strings.Contains(msg, "--allow") {
				t.Errorf("Meldung soll --allow benennen, got %q", msg)
			}
		})
	}
}

// TestUnsetAllowMeansEverythingAllowed hält die Gegenprobe fest: Gar keine
// Policy bleibt "alles erlaubt" — nur die gesetzte leere Liste ist ein Fehler.
func TestUnsetAllowMeansEverythingAllowed(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("contacts", "delete", "42"); code != 0 {
		t.Fatalf("ohne Policy ist alles erlaubt, Exit 0 erwartet, got %d (%s)", code, stderr)
	}
}

// TestSharesUpdateIsExternal: Ein Update kann abgelaufene Links reaktivieren
// und per --remove-password entsperren — das ist eine Wirkung nach außen.
func TestSharesUpdateIsExternal(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("--allow", "read,write", "shares", "update", "7", "--title", "Neu")
	if code != 6 {
		t.Fatalf("Exit 6 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("blockiertes Update darf keinen Request absetzen")
	}

	h2 := newHarness(t)
	if code, _, stderr := h2.run("--allow", "read,write,external", "shares", "update", "7", "--title", "Neu"); code != 0 {
		t.Fatalf("mit external erlaubt, Exit 0 erwartet, got %d (%s)", code, stderr)
	}
}

func TestLocalCommandsAreNotBlockedByReadonly(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("--readonly", "version"); code != 0 {
		t.Fatalf("version ist read, Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if code, _, stderr := h.run("--readonly", "context", "list"); code != 0 {
		t.Fatalf("context list ist read, Exit 0 erwartet, got %d (%s)", code, stderr)
	}
}
