package cli

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// failThenOK antwortet n-mal mit status, danach mit 200.
func failThenOK(n, status int, header http.Header) func(rec *recorded) reply {
	calls := 0
	return func(rec *recorded) reply {
		calls++
		if calls <= n {
			return reply{status: status, body: `{"message":"Bitte später erneut versuchen."}`, header: header}
		}
		return reply{status: 200, body: `[{"id":1}]`}
	}
}

// TestReadsRetryOnTransientErrors: Ein 503 beim Blättern durch 900 Einheiten
// darf den Agenten nicht aus der Schleife werfen.
func TestReadsRetryOnTransientErrors(t *testing.T) {
	h := newHarness(t)
	h.handle = failThenOK(1, 503, nil)
	code, stdout, stderr := h.run("units", "list", "5")
	if code != 0 {
		t.Fatalf("Exit 0 nach Wiederholung erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 2 {
		t.Errorf("zwei Requests erwartet, got %d", len(h.requests))
	}
	if strings.TrimSpace(stdout) != `[{"id":1}]` {
		t.Errorf("Antwort des zweiten Versuchs erwartet, got %q", stdout)
	}
	if len(h.sleeps) != 1 || h.sleeps[0] != time.Second {
		t.Errorf("eine Pause von 1s erwartet, got %v", h.sleeps)
	}
	// Die Wiederholung ist ein Hinweis auf stderr, kein Fehler.
	lines := stderrLines(t, stderr)
	if len(lines) != 1 || lines[0]["warning"] != true {
		t.Fatalf("genau eine Warnzeile erwartet, got %q", stderr)
	}
	if lines[0]["http_status"] != float64(503) || lines[0]["attempt"] != float64(2) {
		t.Errorf("Warnung soll Status und Versuch nennen, got %v", lines[0])
	}
}

func TestReadsHonourRetryAfter(t *testing.T) {
	h := newHarness(t)
	h.handle = failThenOK(1, 429, http.Header{"Retry-After": {"4"}})
	if code, _, stderr := h.run("contacts", "list"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.sleeps) != 1 || h.sleeps[0] != 4*time.Second {
		t.Errorf("Pause aus Retry-After (4s) erwartet, got %v", h.sleeps)
	}
}

func TestReadsGiveUpAfterThreeAttempts(t *testing.T) {
	h := newHarness(t)
	h.handle = failThenOK(99, 503, nil)
	code, _, stderr := h.run("contacts", "list")
	if code != 8 {
		t.Fatalf("Exit 8 nach drei Versuchen erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 3 {
		t.Errorf("drei Versuche erwartet, got %d", len(h.requests))
	}
	if errorLine(t, stderr)["error"] != true {
		t.Error("letzte Zeile soll die Fehlerzeile sein")
	}
}

func TestRetriesFlag(t *testing.T) {
	h := newHarness(t)
	h.handle = failThenOK(99, 503, nil)
	if code, _, _ := h.run("contacts", "list", "--retries", "0"); code != 8 {
		t.Fatalf("Exit 8 erwartet, got %d", code)
	}
	if len(h.requests) != 1 {
		t.Errorf("--retries 0: genau ein Versuch erwartet, got %d", len(h.requests))
	}

	h2 := newHarness(t)
	h2.handle = failThenOK(4, 502, nil)
	if code, _, stderr := h2.run("--retries", "4", "contacts", "list"); code != 0 {
		t.Fatalf("--retries 4: Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h2.requests) != 5 {
		t.Errorf("fünf Versuche erwartet, got %d", len(h2.requests))
	}
}

func TestRetriesFlagRejectsGarbage(t *testing.T) {
	for _, value := range []string{"-1", "drei", "1.5"} {
		h := newHarness(t)
		code, _, stderr := h.run("contacts", "list", "--retries", value)
		if code != 2 {
			t.Errorf("--retries %s: Exit 2 erwartet, got %d (%s)", value, code, stderr)
		}
		if h.last != nil {
			t.Errorf("--retries %s: kein Request erwartet", value)
		}
	}
}

// TestWritesAreNotRetried: Ein POST, dessen Antwort verloren ging, legt bei
// einer Wiederholung doppelt an — auch --retries ändert daran nichts.
func TestWritesAreNotRetried(t *testing.T) {
	h := newHarness(t)
	h.handle = failThenOK(1, 503, nil)
	code, _, _ := h.run("contacts", "create", "--set", "first_name=Ada", "--retries", "3")
	if code != 8 {
		t.Fatalf("Exit 8 erwartet, got %d", code)
	}
	if len(h.requests) != 1 {
		t.Errorf("Schreibaufruf: genau ein Versuch erwartet, got %d", len(h.requests))
	}
}

func TestEscapeHatchGETIsRetried(t *testing.T) {
	h := newHarness(t)
	h.handle = failThenOK(1, 504, nil)
	if code, _, stderr := h.run("api", "GET", "/api/deals"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 2 {
		t.Errorf("zwei Versuche erwartet, got %d", len(h.requests))
	}
}
