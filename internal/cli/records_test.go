package cli

import (
	"strings"
	"testing"
)

// TestInputRejectsTrailingJSON: Ein zweites Objekt in derselben NDJSON-Zeile
// oder ein zweites Array in der JSON-Datei fiel bisher still weg — ein Teil
// der Mieterliste wäre nie importiert worden.
func TestInputRejectsTrailingJSON(t *testing.T) {
	cases := []struct {
		name, file, content string
		args                []string
	}{
		{"NDJSON zwei Objekte", "m.ndjson", `{"extern_id":"A"} {"extern_id":"B"}` + "\n", []string{"units", "import", "5"}},
		{"NDJSON Rest nach Objekt", "m.ndjson", `{"extern_id":"A"}]` + "\n", []string{"units", "import", "5"}},
		{"JSON zwei Arrays", "m.json", `[{"extern_id":"A"}][{"extern_id":"B"}]`, []string{"units", "import", "5"}},
		{"JSON Umschlag mit Rest", "m.json", `{"units":[{"extern_id":"A"}]} {"units":[]}`, []string{"units", "import", "5"}},
		{"fields-set NDJSON", "w.ndjson", `{"extern_id":"RH-0","values":{"a":1}} {"extern_id":"RH-1","values":{"a":2}}` + "\n",
			[]string{"units", "fields-set", "5"}},
		{"fields-set JSON", "w.json", `[{"extern_id":"RH-0","values":{"a":1}}] [{"extern_id":"RH-1","values":{"a":2}}]`,
			[]string{"units", "fields-set", "5"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			file := writeTemp(t, tc.file, tc.content)
			code, _, stderr := h.run(append(tc.args, "--file", file)...)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if len(h.requests) != 0 {
				t.Error("kein Request bei überzähligen Daten")
			}
			msg, _ := errorLine(t, stderr)["message"].(string)
			if !strings.Contains(msg, "genau ein") {
				t.Errorf("Meldung soll „genau ein“ JSON-Wert verlangen, got %q", msg)
			}
		})
	}
}
