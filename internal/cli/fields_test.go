package cli

import (
	"strings"
	"testing"
)

// TestFieldsNeedAnOrganisation: Die Definitionen-Route liest die Organisation
// nur aus Query bzw. Body — ohne sie listet sie die Org des Web-Logins, nicht
// die des Contexts.
func TestFieldsNeedAnOrganisation(t *testing.T) {
	for _, args := range [][]string{
		{"fields", "list", "--model", "unit"},
		{"fields", "create", "--model", "unit", "--name", "X", "--type", "text"},
	} {
		h := newHarness(t)
		delete(h.env, "IMMOJUMP_ORGANISATION_ID")
		code, _, stderr := h.run(args...)
		if code != 3 {
			t.Errorf("%v: Exit 3 erwartet, got %d (%s)", args, code, stderr)
		}
		if h.last != nil {
			t.Errorf("%v: ohne Organisation kein Request erwartet", args)
		}
	}
}

func TestFieldsCreateRejectsInvalidConfig(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("fields", "create", "--model", "unit", "--name", "X", "--type", "dropdown", "--config", "{kaputt")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "--config") {
		t.Errorf("Meldung soll --config nennen, got %q", msg)
	}
}

func TestFieldsRequireModel(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("fields", "list"); code != 2 {
		t.Errorf("ohne --model Exit 2 erwartet, got %d", code)
	}
}

func TestFieldsCreateIsWriteAndNamesAdmin(t *testing.T) {
	spec, ok := Lookup("fields", "create")
	if !ok {
		t.Fatal("fields create fehlt")
	}
	if spec.Risk != RiskWrite {
		t.Errorf("fields create soll write sein, ist %q", spec.Risk)
	}
	if !strings.Contains(spec.Summary, "Admin") {
		t.Errorf("Summary soll die Admin-Pflicht nennen: %q", spec.Summary)
	}
	h := newHarness(t)
	if code, _, _ := h.run("--readonly", "fields", "create", "--model", "unit", "--name", "X", "--type", "text"); code != 6 {
		t.Errorf("--readonly soll fields create sperren, got %d", code)
	}
	h2 := newHarness(t)
	if code, _, _ := h2.run("--readonly", "fields", "list", "--model", "unit"); code != 0 {
		t.Errorf("--readonly soll fields list erlauben, got %d", code)
	}
}
