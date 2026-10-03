package cli

import (
	"strings"
	"testing"
)

// POST /api/units/unit/<immobilie-id> und PUT /api/units/unit/<unit-id> lesen
// genau diese Felder (immo-calc modules/routes/unit_routes.py). Jeden anderen
// Schlüssel verwirft das Backend vor chris2k20/immo-calc#1841 still: Ein Agent
// riet wohnflaeche, miete_kalt oder unit_type und bekam 201 — und eine Einheit
// ohne Fläche und Miete.
var unitBackendFields = []string{
	"einheit", "livingspace", "rooms", "type", "ist_rent", "soll_rent", "soll_rent2",
	"note", "order", "lease_start_date", "last_rent_increase_date",
}

// unitTypes spiegelt das Enum UnitType (immo-calc modules/unit.py). Ein
// anderer Wert („Wohnung“) scheitert vor #1841 erst am Postgres-Enum: 500.
var unitTypes = []string{"wohnen", "gewerbe", "stellplatz", "garage", "sonstiges"}

func unitSpec(t *testing.T, verb string) Spec {
	t.Helper()
	spec, ok := Lookup("units", verb)
	if !ok {
		t.Fatalf("units %s fehlt in der Registry", verb)
	}
	return spec
}

func TestUnitsWriteCommandsOfferEveryBackendField(t *testing.T) {
	known := map[string]bool{}
	for _, field := range unitBackendFields {
		known[field] = true
	}
	for _, verb := range []string{"create", "update"} {
		spec := unitSpec(t, verb)
		keys := map[string]bool{}
		for _, mapping := range spec.Body {
			keys[mapping.Key] = true
			if !known[mapping.Key] {
				t.Errorf("units %s: --%s schreibt %q — das liest das Backend nicht", verb, mapping.Flag, mapping.Key)
			}
		}
		for _, field := range unitBackendFields {
			if !keys[field] {
				t.Errorf("units %s: kein Flag für das Backend-Feld %q", verb, field)
			}
		}
	}
}

func TestUnitsTypeFlagNamesEveryUnitType(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		flag, ok := findFlag(unitSpec(t, verb), "type")
		if !ok {
			t.Fatalf("units %s: --type fehlt", verb)
		}
		for _, value := range unitTypes {
			if !strings.Contains(flag.Desc, value) {
				t.Errorf("units %s: --type nennt %q nicht: %q", verb, value, flag.Desc)
			}
		}
	}
}

// Exposés nennen Kalt- und Warmmiete — ins Feld gehört nur die Kaltmiete.
func TestUnitsIstRentIsTheColdRent(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		flag, ok := findFlag(unitSpec(t, verb), "ist-rent")
		if !ok {
			t.Fatalf("units %s: --ist-rent fehlt", verb)
		}
		if !strings.Contains(flag.Desc, "Kaltmiete") {
			t.Errorf("units %s: --ist-rent soll Kaltmiete sagen: %q", verb, flag.Desc)
		}
	}
}

// Fläche, Zimmer und Mieten sind im Backend Float-Spalten. Ein deutsches
// Dezimalkomma ging per --set als String raus ("62,5") — das Flag fängt es
// ab, bevor ein Request rausgeht.
func TestUnitsNumberFlagsRejectText(t *testing.T) {
	for _, flag := range []string{"livingspace", "rooms", "ist-rent", "soll-rent", "soll-rent2", "order"} {
		t.Run(flag, func(t *testing.T) {
			h := newHarness(t)
			if code, _, stderr := h.run("units", "create", "5", "--"+flag, "62.5"); code != 0 {
				t.Fatalf("mit Dezimalpunkt Exit 0 erwartet, got %d (%s)", code, stderr)
			}
			if !strings.Contains(h.last.Body, ":62.5") {
				t.Errorf("Zahl als JSON-Zahl erwartet, got %s", h.last.Body)
			}

			h = newHarness(t)
			code, _, stderr := h.run("units", "create", "5", "--"+flag, "62,5")
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("ohne gültige Zahl darf kein Request rausgehen")
			}
			msg, _ := errorLine(t, stderr)["message"].(string)
			if !strings.Contains(msg, "--"+flag+" erwartet eine Zahl") {
				t.Errorf("Meldung soll sagen, dass --%s eine Zahl braucht, got %q", flag, msg)
			}
		})
	}
}

// Ein PUT ohne Feld änderte nichts und meldete trotzdem 200.
func TestUnitsUpdateWithoutFieldsIsUsageError(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("units", "update", "9")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("ohne Feld darf kein PUT rausgehen")
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(msg, "--ist-rent") {
		t.Errorf("Meldung soll ein Feld-Flag als Beispiel nennen, got %q", msg)
	}
}

// Jede neu angelegte Immobilie hat schon eine leere „Einheit 1“ (0 m²,
// Miete 0). Wer ein MFH Einheit für Einheit aufbaut, soll sie befüllen, statt
// daneben anzulegen — sonst bleibt sie leer in der Mieterliste stehen.
func TestUnitsHelpExplainsTheDefaultUnit(t *testing.T) {
	for _, verb := range []string{"list", "create"} {
		h := newHarness(t)
		code, help, _ := h.run("units", verb, "--help")
		if code != 0 {
			t.Fatalf("units %s --help: Exit 0 erwartet, got %d", verb, code)
		}
		for _, want := range []string{"Einheit 1", "units update"} {
			if !strings.Contains(help, want) {
				t.Errorf("units %s --help soll %q nennen:\n%s", verb, want, help)
			}
		}
	}
}

// Gemessen gegen die echte API (03.10.2026): Wer „Einheit 1“ per update nur
// mit --ist-rent befüllt, behält dort Soll-Miete 0 — anders als bei create,
// wo beide Soll-Mieten ohne Angabe der Ist-Miete folgen.
func TestUnitsUpdateSaysSollRentsDoNotFollow(t *testing.T) {
	h := newHarness(t)
	_, help, _ := h.run("units", "update", "--help")
	if !strings.Contains(help, "Soll-Mieten ziehen nicht mit") {
		t.Errorf("units update --help soll sagen, dass die Soll-Mieten nicht mitziehen:\n%s", help)
	}
	if example := unitSpec(t, "update").Example; !strings.Contains(example, "--soll-rent ") {
		t.Errorf("das Beispiel befüllt „Einheit 1“ und soll die Soll-Miete mitsetzen: %q", example)
	}
}

// Ohne --order legt die Route mit 0 an; „Einheit 1“ hat 1. Neue Einheiten
// stehen dann in der Mieterliste vor ihr.
func TestUnitsCreateOrderNamesTheDefaultUnit(t *testing.T) {
	flag, ok := findFlag(unitSpec(t, "create"), "order")
	if !ok {
		t.Fatal("units create: --order fehlt")
	}
	if !strings.Contains(flag.Desc, "Einheit 1") {
		t.Errorf("--order soll die Position von „Einheit 1“ nennen: %q", flag.Desc)
	}
}

// Die letzte Einheit einer Immobilie lehnt das Backend beim Löschen mit 400 ab.
func TestUnitsDeleteNamesTheLastUnitRule(t *testing.T) {
	if summary := unitSpec(t, "delete").Summary; !strings.Contains(summary, "letzte") {
		t.Errorf("units delete soll sagen, dass die letzte Einheit bleibt: %q", summary)
	}
}
