package cli

import (
	"strings"
	"testing"
)

func TestTableFlagRendersTable(t *testing.T) {
	h := newHarness(t)
	h.respond = `{"items":[{"id":1,"name":"Ada"}],"total":1}`
	code, stdout, stderr := h.run("--table", "contacts", "list")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "Ada") || strings.Contains(stdout, `{"items"`) {
		t.Errorf("Tabelle erwartet, got %q", stdout)
	}
}

// IMMOJUMP_OUTPUT=table schaltet dasselbe für eine ganze Session — praktisch
// für Menschen, die das CLI dauerhaft am Terminal benutzen.
func TestOutputEnvSwitchesToTable(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_OUTPUT"] = "table"
	h.respond = `[{"id":1,"name":"Ada"}]`
	code, stdout, _ := h.run("contacts", "list")
	if code != 0 || !strings.Contains(stdout, "Ada") || strings.Contains(stdout, `[{"id"`) {
		t.Errorf("Tabelle über die Umgebungsvariable erwartet, got %q", stdout)
	}
}

// Der Default MUSS JSON bleiben. Ein Agent, der ohne Schalter startet, darf
// nie eine Tabelle bekommen — auch nicht in einem PTY.
func TestDefaultStaysJSON(t *testing.T) {
	h := newHarness(t)
	h.respond = `[{"id":1,"name":"Ada"}]`
	_, stdout, _ := h.run("contacts", "list")
	if !strings.HasPrefix(strings.TrimSpace(stdout), "[{") {
		t.Errorf("JSON als Default erwartet, got %q", stdout)
	}
}

// Ein unbekannter Wert darf nicht stillschweigend zu JSON zurückfallen —
// sonst hält der Aufrufer sein Format für gesetzt.
func TestUnknownOutputValueIsAConfigError(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_OUTPUT"] = "yaml"
	code, _, stderr := h.run("contacts", "list")
	if code != 3 {
		t.Fatalf("Exit 3 (Konfigurationsfehler) erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("bei falscher Konfiguration darf kein Request rausgehen")
	}
}

// --table ist ein globales Flag und muss überall erlaubt sein.
func TestTableFlagIsGlobal(t *testing.T) {
	found := false
	for _, flag := range GlobalFlags {
		if flag.Name == "table" {
			found = true
			if flag.Kind != FlagBool {
				t.Errorf("--table soll ein Schalter sein, ist %q", flag.Kind)
			}
		}
	}
	if !found {
		t.Error("--table fehlt in GlobalFlags")
	}
}
