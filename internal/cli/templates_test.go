package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// splitExample zerlegt ein Beispiel wie eine POSIX-Shell, soweit die
// Registry-Beispiele es brauchen: Leerzeichen trennen, einfache Anführungs-
// zeichen klammern.
func splitExample(t *testing.T, example string) []string {
	t.Helper()
	var args []string
	var current strings.Builder
	inQuote, started := false, false
	for _, r := range example {
		switch {
		case r == '\'':
			inQuote, started = !inQuote, true
		case r == ' ' && !inQuote:
			if started {
				args = append(args, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if inQuote {
		t.Fatalf("Beispiel hat ein offenes Anführungszeichen: %q", example)
	}
	if started {
		args = append(args, current.String())
	}
	return args
}

// TestTemplatesBatchMoveExampleMatchesBackend: Das Backend
// (activity_template_routes.batch_move_status) liest target_status_id und
// eine Liste template_ids. Das alte Beispiel schickte from_status_id/
// to_status_id — das Backend antwortete darauf nur mit
// "Target status ID is required".
func TestTemplatesBatchMoveExampleMatchesBackend(t *testing.T) {
	spec, ok := Lookup("templates", "batch-move")
	if !ok {
		t.Fatal("templates batch-move fehlt in der Registry")
	}
	args := splitExample(t, spec.Example)
	if len(args) < 3 || args[0] != "immojump" {
		t.Fatalf("Beispiel beginnt nicht mit immojump templates batch-move: %q", spec.Example)
	}

	h := newHarness(t)
	code, _, stderr := h.run(args[1:]...)
	if code != 0 {
		t.Fatalf("Beispiel läuft nicht durch (exit %d): %s", code, stderr)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(h.last.Body), &body); err != nil {
		t.Fatalf("Body ist kein JSON-Objekt: %q", h.last.Body)
	}
	if _, ok := body["target_status_id"].(float64); !ok {
		t.Errorf("Body braucht eine Zahl target_status_id, hat %q", h.last.Body)
	}
	ids, ok := body["template_ids"].([]any)
	if !ok || len(ids) == 0 {
		t.Errorf("Body braucht eine nicht-leere Liste template_ids, hat %q", h.last.Body)
	}
	for _, stale := range []string{"from_status_id", "to_status_id"} {
		if _, found := body[stale]; found {
			t.Errorf("Body enthält %q, das Backend kennt das Feld nicht: %q", stale, h.last.Body)
		}
	}
}

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// TestTemplateExamplesUseUUIDs: Vorlagen-IDs sind UUIDs. Mit „8“ aus dem
// Beispiel antwortet das Backend auf get mit 404, auf update/delete mit 400.
func TestTemplateExamplesUseUUIDs(t *testing.T) {
	for _, verb := range []string{"get", "update", "delete", "batch-move"} {
		spec, ok := Lookup("templates", verb)
		if !ok {
			t.Fatalf("templates %s fehlt in der Registry", verb)
		}
		if !uuidPattern.MatchString(spec.Example) {
			t.Errorf("templates %s: Beispiel nutzt keine UUID als Vorlagen-ID: %q", verb, spec.Example)
		}
	}
}
