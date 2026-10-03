package cli

import (
	"strings"
	"testing"
)

// Ganze Pipelines anlegen, löschen und importieren dürfen nur Admins der
// Organisation — wie Phasen und Vorlagen schon immer. Jedes andere Mitglied,
// auch ein Bot mit Deny-Rolle, bekommt 403. Vorher konnte jedes Mitglied eine
// Pipeline löschen: Objekte verloren ihre Phase, Phasen und Vorlagen waren
// weg. Ein Agent soll das in --help, docs und schema lesen, nicht erst an der
// 403 lernen. Umbenennen bleibt allen Mitgliedern erlaubt.
func TestPipelineAdminRuleIsInTheSummaries(t *testing.T) {
	cases := []struct {
		verb    string
		phrases []string
	}{
		{"create", []string{"nur Admins"}},
		{"delete", []string{"nur Admins", "Phasen", "Vorlagen"}},
		{"import", []string{"nur Admins"}},
	}
	for _, c := range cases {
		spec, ok := Lookup("pipelines", c.verb)
		if !ok {
			t.Fatalf("pipelines %s fehlt", c.verb)
		}
		for _, phrase := range c.phrases {
			if !strings.Contains(spec.Summary, phrase) {
				t.Errorf("pipelines %s: Summary %q nennt %q nicht", c.verb, spec.Summary, phrase)
			}
		}
	}
}
