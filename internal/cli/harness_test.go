package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded hält den letzten Request, den das CLI abgesetzt hat.
type recorded struct {
	Method      string
	Path        string
	Query       url.Values
	Body        string
	ContentType string
	Header      http.Header
	Multipart   *multipart.Form
	// ContentLength ist -1 bei chunked Transfer (Länge unbekannt).
	ContentLength int64
}

// reply ist eine vorbereitete Antwort für handle.
type reply struct {
	status int
	body   string
	header http.Header
}

// harness startet einen httptest-Server und ruft Run mit einer vollständig
// injizierten Umgebung auf — keine echten Env-Variablen, keine echte Config.
type harness struct {
	t       *testing.T
	server  *httptest.Server
	last    *recorded
	status  int
	respond string
	respCT  string
	env     map[string]string
	stdin   string
	// handle beantwortet einen Request individuell (Blättern, Stapel,
	// Wiederholungen). Ohne handle gilt status/respond für jeden Request.
	handle func(rec *recorded) reply
	// requests sind alle Requests in Reihenfolge, sleeps alle Pausen.
	requests []*recorded
	sleeps   []time.Duration
	mu       sync.Mutex
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, status: 200, respond: `{"ok":true}`, respCT: "application/json"}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &recorded{
			Method:        r.Method,
			Path:          r.URL.Path,
			Query:         r.URL.Query(),
			ContentType:   r.Header.Get("Content-Type"),
			Header:        r.Header.Clone(),
			ContentLength: r.ContentLength,
		}
		mediaType, _, _ := mime.ParseMediaType(rec.ContentType)
		if strings.HasPrefix(mediaType, "multipart/") {
			if err := r.ParseMultipartForm(32 << 20); err == nil {
				rec.Multipart = r.MultipartForm
			}
		} else {
			raw, _ := io.ReadAll(r.Body)
			rec.Body = string(raw)
		}
		h.mu.Lock()
		h.last = rec
		h.requests = append(h.requests, rec)
		h.mu.Unlock()
		status, body := h.status, h.respond
		if h.handle != nil {
			out := h.handle(rec)
			for key, values := range out.header {
				for _, v := range values {
					w.Header().Add(key, v)
				}
			}
			status, body = out.status, out.body
			if status == 0 {
				status = 200
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", h.respCT)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(h.server.Close)

	h.env = map[string]string{
		"IMMOJUMP_BASE_URL":        h.server.URL,
		"IMMOJUMP_EXTRA_BASE_URLS": h.server.URL,
		"IMMOJUMP_TOKEN":           "tok-test",
		"IMMOJUMP_ORGANISATION_ID": "org-test",
		"IMMOJUMP_CONFIG":          filepath.Join(t.TempDir(), "config.json"),
	}
	return h
}

func (h *harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run(args, Options{
		Stdin:  strings.NewReader(h.stdin),
		Stdout: stdout,
		Stderr: stderr,
		Getenv: func(key string) string { return h.env[key] },
		Sleep:  func(d time.Duration) { h.sleeps = append(h.sleeps, d) },
	})
	return code, stdout.String(), stderr.String()
}

// errorLine liest die Fehlerzeile von stderr.
func errorLine(t *testing.T, stderr string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(stderr)
	if line == "" {
		t.Fatal("Fehlerzeile auf stderr erwartet, stderr war leer")
	}
	// Bei mehrzeiliger Ausgabe zählt die letzte Zeile.
	parts := strings.Split(line, "\n")
	var out map[string]any
	if err := json.Unmarshal([]byte(parts[len(parts)-1]), &out); err != nil {
		t.Fatalf("stderr ist kein JSON: %q (%v)", stderr, err)
	}
	return out
}

// sameJSON vergleicht zwei JSON-Dokumente strukturell.
func sameJSON(t *testing.T, got, want string) bool {
	t.Helper()
	if strings.TrimSpace(got) == "" && strings.TrimSpace(want) == "" {
		return true
	}
	var a, b any
	if err := json.Unmarshal([]byte(got), &a); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("erwarteter Body ist kein JSON: %q", want)
	}
	return reflect.DeepEqual(a, b)
}
