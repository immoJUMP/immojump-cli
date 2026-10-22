package cli

import (
	"strings"
	"testing"
)

// filler liefert einen gültigen Platzhalter je Flag-Typ.
func filler(kind FlagKind) string {
	if kind == FlagNumber {
		return "1"
	}
	return "x"
}

// baseArgv baut einen Aufruf mit gefüllten Argumenten und Pflicht-Flags.
func baseArgv(spec Spec, args []string) []string {
	argv := []string{spec.Resource}
	if spec.Verb != "" {
		argv = append(argv, spec.Verb)
	}
	argv = append(argv, args...)
	for _, flag := range spec.Flags {
		if flag.Required {
			argv = append(argv, "--"+flag.Name, filler(flag.Kind))
		}
	}
	return argv
}

func assertEmptyRejected(t *testing.T, label string, argv []string) {
	t.Helper()
	h := newHarness(t)
	code, _, stderr := h.run(argv...)
	if code != 2 {
		t.Errorf("%s: Exit 2 erwartet, got %d (%s)", label, code, stderr)
		return
	}
	if h.last != nil {
		t.Errorf("%s: kein Request erwartet", label)
	}
	if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "leer") {
		t.Errorf("%s: Meldung soll „leer“ sagen, got %q", label, msg)
	}
}

// TestEmptyIDsAreRejectedEverywhere: `--immobilie-id "$ID"` mit leerer
// Variable lud den ganzen Datenraum ohne Immobilie in die Organisation. Kein
// positionales Argument und kein ID-Flag darf leer sein — für jeden Befehl
// der Registry, bevor ein Request rausgeht.
func TestEmptyIDsAreRejectedEverywhere(t *testing.T) {
	for _, spec := range Registry {
		args := make([]string, len(spec.Args))
		for i := range args {
			args[i] = "x"
		}
		if spec.Special == SpecialAPI {
			args = []string{"GET", "/api/x"}
		}
		for i, arg := range spec.Args {
			bad := append([]string(nil), args...)
			bad[i] = "  "
			assertEmptyRejected(t, spec.Name()+" <"+arg.Name+">", baseArgv(spec, bad))
		}
		for _, flag := range spec.Flags {
			if !isIDFlag(flag) {
				continue
			}
			argv := baseArgv(spec, args)
			argv = append(argv, "--"+flag.Name, "")
			assertEmptyRejected(t, spec.Name()+" --"+flag.Name, argv)
		}
	}
	// Variadische Argumente: jedes einzelne zählt.
	assertEmptyRejected(t, "documents analyze 11 \"\"", []string{"documents", "analyze", "11", ""})
	// Globale Flags mit Organisation bzw. Context.
	assertEmptyRejected(t, "--org \"\"", []string{"contacts", "list", "--org", ""})
	assertEmptyRejected(t, "--context \"\"", []string{"contacts", "list", "--context", " "})
}

func TestIDFlagsCoverTheKnownOnes(t *testing.T) {
	want := []string{"immobilie-id", "unit-id", "status-id", "organisation", "account-id"}
	found := map[string]bool{}
	for _, spec := range Registry {
		for _, flag := range spec.Flags {
			if isIDFlag(flag) {
				found[flag.Name] = true
			}
		}
	}
	for _, name := range want {
		if !found[name] {
			t.Errorf("--%s soll als ID-Flag gelten", name)
		}
	}
	// Listen dürfen leer sein: `tags set --tag-ids ""` entfernt alle Tags.
	if spec, _ := Lookup("tags", "set"); func() bool { f, _ := findFlag(spec, "tag-ids"); return isIDFlag(f) }() {
		t.Error("--tag-ids ist eine Liste und darf leer sein")
	}
}

func TestDocumentsUploadWithEmptyImmobilieIDSendsNothing(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A"})
	h := newHarness(t)
	code, _, stderr := h.run("documents", "upload", root, "--immobilie-id", "")
	if code != 2 || h.last != nil {
		t.Fatalf("Exit 2 ohne Request erwartet, got %d (%s)", code, stderr)
	}
	if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "--immobilie-id ist leer") || !strings.Contains(msg, "Variable") {
		t.Errorf("klare Meldung erwartet, got %q", msg)
	}
}
