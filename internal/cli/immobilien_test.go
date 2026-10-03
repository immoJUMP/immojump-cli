package cli

import (
	"strings"
	"testing"
)

// PUT /api/v2/immobilien/{id} liest im Backend ausschließlich status_id. Der
// frühere Befehl `immobilien update` versprach „Immobilie vollständig
// ersetzen“: Jede Feldänderung ging verloren, und ohne status_id nahm das
// Backend das Objekt still aus der Pipeline. `set-status` sagt, was die
// Route tut; Felder ändert `immobilien patch`.

func TestImmobilienUpdateGibtEsNichtMehr(t *testing.T) {
	if _, ok := Lookup("immobilien", "update"); ok {
		t.Fatal("immobilien update darf es nicht wieder geben — PUT setzt nur den Status")
	}
	h := newHarness(t)
	code, _, _ := h.run("immobilien", "update", "5", "--set", "name=MFH")
	if code != 2 {
		t.Fatalf("Exit 2 für einen unbekannten Befehl erwartet, got %d", code)
	}
	if h.last != nil {
		t.Error("für einen unbekannten Befehl darf kein Request rausgehen")
	}
}

// Ohne Ziel-Phase ginge ein Body ohne status_id raus: Das Backend antwortet
// darauf inzwischen mit 400, ältere Instanzen löschten den Status.
func TestImmobilienSetStatusOhneZielIstUsageFehler(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("immobilien", "set-status", "5")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("ohne Ziel-Phase darf kein PUT rausgehen")
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	for _, hint := range []string{"--status-id", "--remove-status"} {
		if !strings.Contains(msg, hint) {
			t.Errorf("Meldung soll %s nennen, got %q", hint, msg)
		}
	}
}

func TestImmobilienSetStatusUndRemoveStatusSchliessenSichAus(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.run("immobilien", "set-status", "5", "--status-id", "7", "--remove-status")
	if code != 2 {
		t.Fatalf("Exit 2 für widersprüchliche Flags erwartet, got %d", code)
	}
	if h.last != nil {
		t.Error("bei widersprüchlichen Flags darf kein Request rausgehen")
	}
}

func TestImmobilienSetStatusVerlangtEineZahl(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.run("immobilien", "set-status", "5", "--status-id", "Besichtigung")
	if code != 2 {
		t.Fatalf("Exit 2 für eine Phase als Name statt ID erwartet, got %d", code)
	}
	if h.last != nil {
		t.Error("ohne gültige ID darf kein Request rausgehen")
	}
}
