package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("Body ist kein JSON-Objekt: %q (%v)", raw, err)
	}
	return out
}

func TestSharesCreateItemSugar(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("shares", "create",
		"--immobilie", "5",
		"--dokument", "d1",
		"--dokument", "d2",
		"--bild", "b1")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	want := []any{
		map[string]any{"entity_type": "immobilie", "entity_id": "5"},
		map[string]any{"entity_type": "dokument", "entity_id": "d1"},
		map[string]any{"entity_type": "dokument", "entity_id": "d2"},
		map[string]any{"entity_type": "bild", "entity_id": "b1"},
	}
	if !reflect.DeepEqual(body["items"], want) {
		t.Errorf("items\n got: %#v\nwant: %#v", body["items"], want)
	}
}

func TestSharesCreateFullFlagSet(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("shares", "create",
		"--immobilie", "5",
		"--title", "Finanzierungsunterlagen",
		"--note", "Anbei die Unterlagen.",
		"--password", "bank2026",
		"--expires-at", "2026-09-30",
		"--recipient-email", "bank@example.com",
		"--send-email",
		"--include-password-in-email",
		"--show-key-facts")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	checks := map[string]any{
		"title":                     "Finanzierungsunterlagen",
		"note":                      "Anbei die Unterlagen.",
		"password":                  "bank2026",
		"expires_at":                "2026-09-30T23:59:59",
		"recipient_email":           "bank@example.com",
		"send_email":                true,
		"include_password_in_email": true,
	}
	for key, want := range checks {
		if got := body[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %#v erwartet, got %#v", key, want, got)
		}
	}
	settings, ok := body["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings-Objekt erwartet, got %#v", body["settings"])
	}
	if settings["show_key_facts"] != true {
		t.Errorf("settings.show_key_facts=true erwartet, got %#v", settings["show_key_facts"])
	}
}

func TestSharesCreateExpiresAtPassthroughForFullTimestamps(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("shares", "create", "--immobilie", "5",
		"--expires-at", "2026-09-30T12:00:00+02:00"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	if body["expires_at"] != "2026-09-30T12:00:00+02:00" {
		t.Errorf("vollständiger Zeitstempel unverändert erwartet, got %#v", body["expires_at"])
	}
}

func TestSharesCreateWithoutItemsIsUsageError(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("shares", "create", "--title", "Ohne Inhalt")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d", code)
	}
	if h.last != nil {
		t.Error("ohne Items darf kein Request rausgehen")
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if msg == "" {
		t.Error("erklärende Meldung erwartet")
	}
}

func TestSharesCreateRejectsUnknownEntityTypeViaBody(t *testing.T) {
	// Der Sugar-Pfad kennt nur immobilie/dokument/bild; --body bleibt frei
	// (das Backend validiert), aber die Sugar-Flags müssen sauber bleiben.
	h := newHarness(t)
	code, _, _ := h.run("shares", "create", "--body", `{"items":[{"entity_type":"kontakt","entity_id":"1"}]}`)
	if code != 0 {
		t.Fatalf("--body wird unverändert durchgereicht, Exit 0 erwartet, got %d", code)
	}
	body := decodeBody(t, h.last.Body)
	items := body["items"].([]any)
	if items[0].(map[string]any)["entity_type"] != "kontakt" {
		t.Error("--body soll unverändert durchgereicht werden")
	}
}

func TestSharesCreateSugarOverlaysBody(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("shares", "create",
		"--body", `{"title":"Alt","note":"Bleibt"}`,
		"--immobilie", "5",
		"--title", "Neu"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	if body["title"] != "Neu" {
		t.Errorf("Sugar überlagert --body, got %#v", body["title"])
	}
	if body["note"] != "Bleibt" {
		t.Errorf("nicht überlagerte Felder bleiben, got %#v", body["note"])
	}
}

func TestSharesUpdateSendsOnlySetKeys(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("shares", "update", "7", "--title", "Neuer Titel"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	if len(body) != 1 || body["title"] != "Neuer Titel" {
		t.Errorf("nur title im PATCH erwartet, got %#v", body)
	}
}

func TestSharesUpdateEmptyNoteIsSent(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("shares", "update", "7", "--note", ""); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	got, ok := body["note"]
	if !ok {
		t.Fatalf("note muss im Body stehen, got %#v", body)
	}
	if got != "" {
		t.Errorf("leerer String erwartet, got %#v", got)
	}
}

func TestSharesUpdateRemovePasswordSendsNull(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("shares", "update", "7", "--remove-password"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Body != `{"password":null}` {
		t.Errorf(`{"password":null} erwartet, got %s`, h.last.Body)
	}
}

// TestSharesUpdateNormalizesExpiresAtToEndOfDay: "gültig bis einschließlich"
// — identische Semantik zur Web-App (ShareLinkService.resolveExpiresAt).
func TestSharesUpdateNormalizesExpiresAtToEndOfDay(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("shares", "update", "7", "--expires-at", "2026-12-24"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	body := decodeBody(t, h.last.Body)
	if body["expires_at"] != "2026-12-24T23:59:59" {
		t.Errorf("Tagesende erwartet, got %#v", body["expires_at"])
	}
}

// TestSharesExpiresAtVariants hält die komplette Normalisierung fest: nur ein
// reines Datum wird angefasst, alles andere geht unverändert ans Backend.
func TestSharesExpiresAtVariants(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-09-30", "2026-09-30T23:59:59"},
		{"2026-01-01", "2026-01-01T23:59:59"},
		{"2026-09-30T12:00:00+02:00", "2026-09-30T12:00:00+02:00"},
		{"2026-09-30T12:00:00Z", "2026-09-30T12:00:00Z"},
		{"2026-09-30T23:59:59", "2026-09-30T23:59:59"},
		{"30.09.2026", "30.09.2026"}, // kein ISO-Datum: Backend validiert
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			h := newHarness(t)
			if code, _, stderr := h.run("shares", "update", "7", "--expires-at", tc.in); code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
			}
			if got := decodeBody(t, h.last.Body)["expires_at"]; got != tc.want {
				t.Errorf("%q erwartet, got %#v", tc.want, got)
			}
		})
	}
}

// TestSharesRejectsEmptyPassword: "" wäre ein Passwort der Länge 0 — das
// Backend lehnt es ab (min. 4 Zeichen), und gemeint ist meistens etwas anderes.
func TestSharesRejectsEmptyPassword(t *testing.T) {
	cases := []struct {
		name string
		args []string
		hint string
	}{
		{"update", []string{"shares", "update", "7", "--password", ""}, "--remove-password"},
		{"update mit Leerzeichen", []string{"shares", "update", "7", "--password", "   "}, "--remove-password"},
		{"create", []string{"shares", "create", "--immobilie", "5", "--password", ""}, "weglassen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.args...)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("bei leerem Passwort darf kein Request rausgehen")
			}
			msg, _ := errorLine(t, stderr)["message"].(string)
			if !strings.Contains(msg, tc.hint) {
				t.Errorf("Meldung soll %q vorschlagen, got %q", tc.hint, msg)
			}
		})
	}
}

func TestSharesUpdateWithoutAnyFieldIsUsageError(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("shares", "update", "7")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d", code)
	}
	if h.last != nil {
		t.Error("ohne Änderung darf kein PATCH rausgehen")
	}
	if errorLine(t, stderr)["message"] == "" {
		t.Error("erklärende Meldung erwartet")
	}
}

func TestSharesUpdatePasswordAndRemovePasswordConflict(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.run("shares", "update", "7", "--password", "neu", "--remove-password")
	if code != 2 {
		t.Fatalf("Exit 2 für widersprüchliche Flags erwartet, got %d", code)
	}
}
