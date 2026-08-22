package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRootHelpListsResources(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("--help")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	for _, want := range []string{"contacts", "immobilien", "shares", "api", "docs", "schema", "Globale Flags", "--readonly"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("Root-Hilfe soll %q enthalten:\n%s", want, stdout)
		}
	}
	if h.last != nil {
		t.Error("Hilfe darf keinen Request absetzen")
	}
}

func TestBareInvocationShowsHelp(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run()
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if !strings.Contains(stdout, "immojump") {
		t.Errorf("Hilfe erwartet, got %q", stdout)
	}
}

func TestResourceHelpListsVerbs(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("shares", "--help")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	for _, want := range []string{"list", "create", "update", "revoke"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("Ressourcen-Hilfe soll Verb %q zeigen:\n%s", want, stdout)
		}
	}
}

func TestCommandHelpShowsArgsFlagsRiskAndExample(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("shares", "create", "--help")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	for _, want := range []string{"--immobilie", "--password", "--expires-at", "external", "POST /api/share-links", "Beispiel"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("Befehls-Hilfe soll %q enthalten:\n%s", want, stdout)
		}
	}
}

func TestHelpVerbAlias(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("help", "contacts")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if !strings.Contains(stdout, "set-status") {
		t.Errorf("help <resource> erwartet, got %q", stdout)
	}
}

func TestHelpForCommandWithoutVerb(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("api", "--help")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if !strings.Contains(stdout, "METHOD") && !strings.Contains(stdout, "method") {
		t.Errorf("api-Hilfe soll die Argumente zeigen, got %q", stdout)
	}
}

func TestVersionCommand(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("version")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("Version erwartet")
	}

	code, flagOut, _ := h.run("--version")
	if code != 0 || strings.TrimSpace(flagOut) == "" {
		t.Errorf("--version soll dasselbe tun, got %d / %q", code, flagOut)
	}
}

func TestDocsRendersMarkdownForEveryCommand(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("docs")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !strings.HasPrefix(stdout, "# ") {
		t.Errorf("Markdown-Überschrift erwartet, got %q", stdout[:min(40, len(stdout))])
	}
	for _, spec := range Registry {
		needle := spec.Resource
		if spec.Verb != "" {
			needle += " " + spec.Verb
		}
		if !strings.Contains(stdout, needle) {
			t.Errorf("Referenz soll %q dokumentieren", needle)
		}
	}
	for _, want := range []string{"Exit-Code", "Risk", "IMMOJUMP_TOKEN", "--readonly"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("Referenz soll %q erklären", want)
		}
	}
}

// TestDocsFilters: `schema` kann seit jeher einen Ausschnitt liefern, `docs`
// dumpte immer alles. Für ein Werkzeug, dessen Verkaufsargument
// Kontext-Ökonomie ist, war das die teuerste Inkonsistenz.
func TestDocsFilters(t *testing.T) {
	h := newHarness(t)
	_, full, _ := h.run("docs")

	code, scoped, stderr := h.run("docs", "shares")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !strings.HasPrefix(scoped, "# ") {
		t.Errorf("Markdown-Überschrift erwartet, got %q", scoped[:min(40, len(scoped))])
	}
	for _, want := range []string{"shares create", "shares revoke", "--immobilie"} {
		if !strings.Contains(scoped, want) {
			t.Errorf("Ausschnitt soll %q dokumentieren:\n%s", want, scoped)
		}
	}
	if strings.Contains(scoped, "contacts create") {
		t.Error("der Ausschnitt soll nur shares zeigen")
	}
	if len(scoped) >= len(full) {
		t.Errorf("Ausschnitt soll kürzer sein als die Vollreferenz (%d vs. %d)", len(scoped), len(full))
	}

	code, one, _ := h.run("docs", "shares", "create")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if !strings.Contains(one, "shares create") {
		t.Errorf("shares create erwartet:\n%s", one)
	}
	if strings.Contains(one, "shares revoke") {
		t.Errorf("nur den einen Befehl erwartet:\n%s", one)
	}
	if len(one) >= len(scoped) {
		t.Errorf("ein Befehl soll kürzer sein als die ganze Ressource (%d vs. %d)", len(one), len(scoped))
	}
}

func TestDocsUnknownScopeExitsWith2(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"docs", "gibtsnicht"}, {"docs", "shares", "gibtsnicht"}} {
		code, stdout, stderr := h.run(args...)
		if code != 2 {
			t.Errorf("%v: Exit 2 erwartet, got %d", args, code)
		}
		if stdout != "" {
			t.Errorf("%v: kein stdout bei Bedienfehlern erwartet, got %q", args, stdout)
		}
		if errorLine(t, stderr)["message"] == "" {
			t.Errorf("%v: erklärende Meldung erwartet", args)
		}
	}
}

// TestUnscopedDumpsHintAtTheCheapForm: Der volle Dump ist teuer — wer ihn
// zieht, soll wenigstens erfahren, dass es die gezielte Form gibt. Auf
// stderr, damit `immojump docs > REFERENCE.md` sauber bleibt.
func TestUnscopedDumpsHintAtTheCheapForm(t *testing.T) {
	for _, command := range []string{"docs", "schema"} {
		h := newHarness(t)
		code, stdout, stderr := h.run(command)
		if code != 0 {
			t.Fatalf("%s: Exit 0 erwartet, got %d", command, code)
		}
		if strings.Contains(stdout, "Hinweis") || strings.Contains(stdout, "hinweis") {
			t.Errorf("%s: stdout bleibt frei von Hinweisen", command)
		}
		line := stderrLines(t, stderr)
		if len(line) != 1 || line[0]["warning"] != true {
			t.Fatalf("%s: eine Hinweiszeile erwartet, got %q", command, stderr)
		}
		message, _ := line[0]["message"].(string)
		if !strings.Contains(message, "immojump "+command+" <ressource>") {
			t.Errorf("%s: der Hinweis soll die gezielte Form nennen, got %q", command, message)
		}
	}
}

func TestScopedDumpsStayQuiet(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"docs", "shares"}, {"schema", "shares"}, {"schema", "shares", "create"}} {
		if _, _, stderr := h.run(args...); stderr != "" {
			t.Errorf("%v: kein Hinweis nötig, got %q", args, stderr)
		}
	}
}

func TestSchemaIsValidJSON(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("schema")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	var doc struct {
		Version     string            `json:"version"`
		ExitCodes   map[string]string `json:"exit_codes"`
		GlobalFlags []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"global_flags"`
		Commands []struct {
			Resource string `json:"resource"`
			Verb     string `json:"verb"`
			Method   string `json:"method"`
			Path     string `json:"path"`
			Risk     string `json:"risk"`
			Summary  string `json:"summary"`
			Args     []struct {
				Name string `json:"name"`
			} `json:"args"`
			Flags []struct {
				Name string `json:"name"`
			} `json:"flags"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("schema soll JSON liefern: %v", err)
	}
	if len(doc.Commands) != len(Registry) {
		t.Errorf("%d Befehle erwartet, got %d", len(Registry), len(doc.Commands))
	}
	if len(doc.ExitCodes) == 0 {
		t.Error("exit_codes erwartet")
	}
	if len(doc.GlobalFlags) == 0 {
		t.Error("global_flags erwartet")
	}
	if doc.ExitCodes["6"] == "" {
		t.Error("Exit-Code 6 soll dokumentiert sein")
	}
}

func TestSchemaFilters(t *testing.T) {
	h := newHarness(t)
	_, stdout, _ := h.run("schema", "shares")
	var byResource struct {
		Commands []struct {
			Resource string `json:"resource"`
			Verb     string `json:"verb"`
			Risk     string `json:"risk"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &byResource); err != nil {
		t.Fatalf("JSON erwartet: %v", err)
	}
	if len(byResource.Commands) != 4 {
		t.Fatalf("vier shares-Befehle erwartet, got %d", len(byResource.Commands))
	}
	for _, cmd := range byResource.Commands {
		if cmd.Resource != "shares" {
			t.Errorf("nur shares erwartet, got %q", cmd.Resource)
		}
	}

	_, stdout, _ = h.run("schema", "shares", "create")
	if err := json.Unmarshal([]byte(stdout), &byResource); err != nil {
		t.Fatalf("JSON erwartet: %v", err)
	}
	if len(byResource.Commands) != 1 || byResource.Commands[0].Verb != "create" {
		t.Fatalf("genau shares create erwartet, got %+v", byResource.Commands)
	}
	if byResource.Commands[0].Risk != "external" {
		t.Errorf("Risk external erwartet, got %q", byResource.Commands[0].Risk)
	}
}

func TestSchemaUnknownResourceExitsWith2(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("schema", "gibtsnicht"); code != 2 {
		t.Fatal("Exit 2 erwartet")
	}
}

func TestDocsAndSchemaWorkWithoutToken(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	delete(h.env, "IMMOJUMP_ORGANISATION_ID")
	for _, args := range [][]string{{"docs"}, {"schema"}, {"version"}, {"--help"}} {
		if code, _, stderr := h.run(args...); code != 0 {
			t.Errorf("%v ohne Token: Exit 0 erwartet, got %d (%s)", args, code, stderr)
		}
	}
}
