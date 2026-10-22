package cli

import (
	"strings"
	"testing"
)

// TestBoolFlagsRejectUnknownValues: `--dry-run=tru` wurde still zu false —
// und der „Trockenlauf“ schrieb echt. Jeder Schalter nimmt nur true/false/1/0
// (oder steht ohne Wert), für jeden Befehl und jedes globale Flag.
func TestBoolFlagsRejectUnknownValues(t *testing.T) {
	check := func(label string, argv []string) {
		t.Helper()
		h := newHarness(t)
		code, _, stderr := h.run(argv...)
		if code != 2 || h.last != nil {
			t.Errorf("%s: Exit 2 ohne Request erwartet, got %d (%s)", label, code, stderr)
			return
		}
		if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "true") {
			t.Errorf("%s: Meldung soll die erlaubten Werte nennen, got %q", label, msg)
		}
	}
	for _, spec := range Registry {
		args := make([]string, len(spec.Args))
		for i := range args {
			args[i] = "x"
		}
		if spec.Special == SpecialAPI {
			args = []string{"GET", "/api/x"}
		}
		for _, flag := range spec.Flags {
			if flag.Kind != FlagBool {
				continue
			}
			check(spec.Name()+" --"+flag.Name+"=tru", append(baseArgv(spec, args), "--"+flag.Name+"=tru"))
		}
	}
	for _, flag := range GlobalFlags {
		if flag.Kind == FlagBool {
			check("--"+flag.Name+"=ja", []string{"contacts", "list", "--" + flag.Name + "=ja"})
		}
	}
}

func TestBoolFlagsAcceptTheirValues(t *testing.T) {
	for value, want := range map[string]string{"true": "true", "TRUE": "true", "1": "true", "false": "false", "False": "false", "0": "false"} {
		h := newHarness(t)
		if code, _, stderr := h.run("units", "list", "5", "--vacant="+value); code != 0 {
			t.Errorf("--vacant=%s: Exit 0 erwartet, got %d (%s)", value, code, stderr)
			continue
		}
		if got := h.last.Query.Get("vacant"); got != want {
			t.Errorf("--vacant=%s: vacant=%s erwartet, got %q", value, want, got)
		}
	}
}
