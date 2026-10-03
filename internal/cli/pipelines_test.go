package cli

import (
	"strings"
	"testing"
)

// Das Backend sperrt zwei Strukturänderungen (immo-calc#1854): Den Typ einer
// Pipeline ändern nur Admins, und nur solange alle Phasen schon den neuen Typ
// haben. Eine Phase zieht per pipeline_id nur in eine Pipeline derselben
// Organisation und desselben Typs um. Ein Agent soll das vor dem Aufruf in
// --help, docs und schema lesen, nicht erst an der 400 lernen.
func TestPipelineStructureRulesAreInTheSummaries(t *testing.T) {
	cases := []struct {
		resource, verb string
		phrases        []string
	}{
		{"pipelines", "update", []string{"entity_type", "Admin", "Phasen"}},
		{"statuses", "update", []string{"pipeline_id", "Organisation", "Typ"}},
	}
	for _, c := range cases {
		spec, ok := Lookup(c.resource, c.verb)
		if !ok {
			t.Fatalf("%s %s fehlt", c.resource, c.verb)
		}
		for _, phrase := range c.phrases {
			if !strings.Contains(spec.Summary, phrase) {
				t.Errorf("%s %s: Summary %q nennt %q nicht", c.resource, c.verb, spec.Summary, phrase)
			}
		}
	}
}
