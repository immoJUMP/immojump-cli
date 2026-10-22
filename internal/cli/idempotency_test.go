package cli

import (
	"fmt"
	"path/filepath"
	"testing"
)

func idempotencyKeys(h *harness, method string) []string {
	var keys []string
	for _, rec := range h.requests {
		if rec.Method == method {
			keys = append(keys, rec.Header.Get("Idempotency-Key"))
		}
	}
	return keys
}

// TestIdempotencyKeyIsDerivedPerRequest: Derselbe Schlüssel für jeden Stapel
// oder jedes Dokument hieße für ein Backend, das ihn auswertet: alles nach
// dem ersten Request ist eine Wiederholung und wird verworfen.
func TestIdempotencyKeyIsDerivedPerRequest(t *testing.T) {
	t.Run("upload", func(t *testing.T) {
		root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
		h := newHarness(t)
		b := &uploadBackend{}
		h.handle = b.handle
		if code, _, stderr := h.run("documents", "upload", root, "--batch-files", "1", "--idempotency-key", "K"); code != 0 {
			t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
		}
		if got := fmt.Sprint(idempotencyKeys(h, "POST")); got != "[K-b1 K-b2]" {
			t.Errorf("K-b1, K-b2 erwartet, got %s", got)
		}
	})
	t.Run("upload einzeln", func(t *testing.T) {
		root := dataroom(t, map[string]string{"a.pdf": "A"})
		h := newHarness(t)
		if code, _, _ := h.run("documents", "upload", filepath.Join(root, "a.pdf"), "--idempotency-key", "K"); code != 0 {
			t.Fatalf("Exit 0 erwartet, got %d", code)
		}
		if got := fmt.Sprint(idempotencyKeys(h, "POST")); got != "[K]" {
			t.Errorf("ein Request behält den Schlüssel unverändert, got %s", got)
		}
	})
	t.Run("analyze", func(t *testing.T) {
		h := newHarness(t)
		b := &analyzeBackend{}
		h.handle = b.handle
		if code, _, stderr := h.run("documents", "analyze", "d1", "d2", "--idempotency-key", "K"); code != 0 {
			t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
		}
		if got := fmt.Sprint(idempotencyKeys(h, "POST")); got != "[K-d1 K-d2]" {
			t.Errorf("K-d1, K-d2 erwartet, got %s", got)
		}
	})
	t.Run("units import", func(t *testing.T) {
		h := newHarness(t)
		b := &bulkBackend{}
		h.handle = b.handle
		file := writeTemp(t, "m.ndjson", ndjsonUnits(3))
		if code, _, stderr := h.run("units", "import", "5", "--file", file, "--batch-size", "2", "--idempotency-key", "K"); code != 0 {
			t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
		}
		if got := fmt.Sprint(idempotencyKeys(h, "POST")); got != "[K-b1 K-b2]" {
			t.Errorf("K-b1, K-b2 erwartet, got %s", got)
		}
	})
	t.Run("fields-set", func(t *testing.T) {
		h := newHarness(t)
		b := &fieldsBackend{units: []string{"u0"}}
		h.handle = b.handle
		file := writeTemp(t, "w.ndjson", `{"extern_id":"RH-0","values":{"a":1}}`)
		if code, _, stderr := h.run("units", "fields-set", "5", "--file", file, "--idempotency-key", "K"); code != 0 {
			t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
		}
		if got := fmt.Sprint(idempotencyKeys(h, "PUT")); got != "[K-b1]" {
			t.Errorf("K-b1 erwartet, got %s", got)
		}
	})
}
