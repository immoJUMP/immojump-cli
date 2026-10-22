package api

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// scripted antwortet der Reihe nach mit den angegebenen Status-Codes; danach
// immer mit dem letzten.
func scripted(t *testing.T, statuses []int, header http.Header) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(atomic.AddInt32(&calls, 1)) - 1
		if n >= len(statuses) {
			n = len(statuses) - 1
		}
		for key, values := range header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statuses[n])
		if statuses[n] >= 400 {
			_, _ = w.Write([]byte(`{"message":"vorübergehend"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// retryClient wiederholt bis zu zweimal und schläft nicht wirklich.
func retryClient(srv *httptest.Server, sleeps *[]time.Duration) *Client {
	c := testClient(srv)
	c.Retries = 2
	c.Sleep = func(d time.Duration) { *sleeps = append(*sleeps, d) }
	return c
}

func TestGETRetriesOnTransientStatusThenSucceeds(t *testing.T) {
	for _, status := range []int{429, 502, 503, 504} {
		srv, calls := scripted(t, []int{status, 200}, nil)
		var sleeps []time.Duration
		res, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/contacts"})
		if err != nil {
			t.Fatalf("%d: Erfolg nach Wiederholung erwartet, got %v", status, err)
		}
		if res.Status != 200 || atomic.LoadInt32(calls) != 2 {
			t.Errorf("%d: zwei Aufrufe und 200 erwartet, got %d Aufrufe, Status %d", status, *calls, res.Status)
		}
		if len(sleeps) != 1 || sleeps[0] != time.Second {
			t.Errorf("%d: eine Pause von 1s erwartet, got %v", status, sleeps)
		}
	}
}

func TestRetryBacksOffExponentiallyAndGivesUp(t *testing.T) {
	srv, calls := scripted(t, []int{503}, nil)
	var sleeps []time.Duration
	_, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/contacts"})
	if err == nil {
		t.Fatal("Fehler nach drei Versuchen erwartet")
	}
	if got := atomic.LoadInt32(calls); got != 3 {
		t.Errorf("genau drei Versuche erwartet (1 + 2 Wiederholungen), got %d", got)
	}
	if len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != 2*time.Second {
		t.Errorf("Pausen 1s, 2s erwartet, got %v", sleeps)
	}
	if ExitCodeFor(err) != 8 {
		t.Errorf("der letzte Fehler zählt: Exit 8 erwartet, got %d", ExitCodeFor(err))
	}
}

func TestRetryHonoursRetryAfterSeconds(t *testing.T) {
	srv, _ := scripted(t, []int{429, 200}, http.Header{"Retry-After": {"7"}})
	var sleeps []time.Duration
	if _, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/x"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] != 7*time.Second {
		t.Errorf("Retry-After 7 → Pause 7s erwartet, got %v", sleeps)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	srv, _ := scripted(t, []int{429, 200}, http.Header{"Retry-After": {"3600"}})
	var sleeps []time.Duration
	if _, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/x"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] != MaxRetryWait {
		t.Errorf("Pause auf %v gedeckelt erwartet, got %v", MaxRetryWait, sleeps)
	}
}

func TestRetryAfterAsHTTPDate(t *testing.T) {
	when := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	srv, _ := scripted(t, []int{503, 200}, http.Header{"Retry-After": {when}})
	var sleeps []time.Duration
	if _, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/x"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] < 3*time.Second || sleeps[0] > 6*time.Second {
		t.Errorf("Pause um 5s aus dem HTTP-Datum erwartet, got %v", sleeps)
	}
}

func TestNoRetryOn500AndOtherErrors(t *testing.T) {
	for _, status := range []int{500, 400, 404, 409} {
		srv, calls := scripted(t, []int{status, 200}, nil)
		var sleeps []time.Duration
		if _, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/x"}); err == nil {
			t.Fatalf("%d: Fehler erwartet", status)
		}
		if got := atomic.LoadInt32(calls); got != 1 {
			t.Errorf("%d: keine Wiederholung erwartet, got %d Aufrufe", status, got)
		}
	}
}

// TestWritesAreNeverRetriedImplicitly: Ein POST, der beim Server angekommen
// ist, aber dessen Antwort verloren ging, legt bei einer Wiederholung doppelt
// an. Nur ausdrücklich idempotente Aufrufe dürfen wiederholt werden.
func TestWritesAreNeverRetriedImplicitly(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		srv, calls := scripted(t, []int{503, 200}, nil)
		var sleeps []time.Duration
		if _, err := retryClient(srv, &sleeps).Do(Request{Method: method, Path: "/api/x", Body: []byte(`{}`)}); err == nil {
			t.Fatalf("%s: Fehler ohne Wiederholung erwartet", method)
		}
		if got := atomic.LoadInt32(calls); got != 1 {
			t.Errorf("%s: genau ein Aufruf erwartet, got %d", method, got)
		}
	}
}

func TestIdempotentWriteIsRetried(t *testing.T) {
	srv, calls := scripted(t, []int{502, 200}, nil)
	var sleeps []time.Duration
	_, err := retryClient(srv, &sleeps).Do(Request{
		Method: "POST", Path: "/api/x", Body: []byte(`{"units":[]}`), Idempotent: true,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("zwei Aufrufe erwartet, got %d", got)
	}
}

func TestRetryResendsTheBody(t *testing.T) {
	var bodies []string
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		bodies = append(bodies, string(raw))
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var sleeps []time.Duration
	_, err := retryClient(srv, &sleeps).Do(Request{
		Method: "POST", Path: "/api/x", Body: []byte(`{"a":1}`), Idempotent: true,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(bodies) != 2 || bodies[0] != `{"a":1}` || bodies[1] != `{"a":1}` {
		t.Errorf("Body bei jedem Versuch vollständig erwartet, got %q", bodies)
	}
}

func TestRetryOnNetworkError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// Verbindung hart kappen: Der Client sieht einen Transportfehler.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.(*net.TCPConn).SetLinger(0)
				_ = conn.Close()
			}
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var sleeps []time.Duration
	if _, err := retryClient(srv, &sleeps).Do(Request{Method: "GET", Path: "/api/x"}); err != nil {
		t.Fatalf("Erfolg nach Netzwerkfehler erwartet, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("zwei Aufrufe erwartet, got %d", got)
	}
}

func TestOnRetryReportsEachRepetition(t *testing.T) {
	srv, _ := scripted(t, []int{503, 503, 200}, nil)
	var sleeps []time.Duration
	c := retryClient(srv, &sleeps)
	var seen []string
	c.OnRetry = func(attempt, attempts int, wait time.Duration, err *Error) {
		seen = append(seen, time.Duration(attempt).String()+"/"+time.Duration(attempts).String())
		if err.Status != 503 {
			t.Errorf("Status 503 im Hook erwartet, got %d", err.Status)
		}
	}
	if _, err := c.Do(Request{Method: "GET", Path: "/api/x"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(seen) != 2 {
		t.Errorf("zwei Hook-Aufrufe erwartet, got %v", seen)
	}
}

func TestZeroRetriesMeansOneAttempt(t *testing.T) {
	srv, calls := scripted(t, []int{503, 200}, nil)
	c := testClient(srv)
	c.Sleep = func(time.Duration) { t.Error("ohne Wiederholungen darf nicht geschlafen werden") }
	if _, err := c.Do(Request{Method: "GET", Path: "/api/x"}); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("ein Aufruf erwartet, got %d", got)
	}
}

func TestErrorCarriesRetryAfter(t *testing.T) {
	srv, _ := scripted(t, []int{429}, http.Header{"Retry-After": {"12"}})
	_, err := testClient(srv).Do(Request{Method: "POST", Path: "/api/x", Body: []byte(`{}`)})
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("*Error erwartet, got %T", err)
	}
	if apiErr.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter 12s erwartet, got %v", apiErr.RetryAfter)
	}
}

func TestResponseCarriesHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upload-Empty-Files", `["a.pdf"]`)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	res, err := testClient(srv).Do(Request{Method: "GET", Path: "/api/x"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := res.Header.Get("X-Upload-Empty-Files"); got != `["a.pdf"]` {
		t.Errorf("Header in der Antwort erwartet, got %q", got)
	}
}

// TestMultipartStreamsFromDisk: Dateien kommen von der Platte, mit eigener
// Content-Type-Angabe und vorab bekannter Länge.
func TestMultipartStreamsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gross.pdf"
	content := strings.Repeat("%PDF-1.4 ", 100000)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var gotLength int64
	var gotContent, gotType, gotField string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLength = r.ContentLength
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		header := r.MultipartForm.File["files[]"][0]
		gotType = header.Header.Get("Content-Type")
		f, _ := header.Open()
		raw, _ := io.ReadAll(f)
		gotContent = string(raw)
		gotField = r.FormValue("report")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	_, err := testClient(srv).Do(Request{
		Method: "POST", Path: "/api/documents/documents/bulk-upload",
		Multipart: &Multipart{
			Files:  []FilePart{{Field: "files[]", Filename: `Vertrag "A".pdf`, Path: path, ContentType: "application/pdf"}},
			Fields: map[string]string{"report": "true"},
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotContent != content || gotType != "application/pdf" || gotField != "true" {
		t.Errorf("Datei/Typ/Feld falsch übertragen: %d Bytes, %q, %q", len(gotContent), gotType, gotField)
	}
	if gotLength <= int64(len(content)) {
		t.Errorf("Content-Length vorab erwartet, got %d", gotLength)
	}
}

func TestMultipartMissingFileIsLocalError(t *testing.T) {
	srv, calls := scripted(t, []int{200}, nil)
	_, err := testClient(srv).Do(Request{
		Method: "POST", Path: "/api/x",
		Multipart: &Multipart{Files: []FilePart{{Field: "files[]", Filename: "weg.pdf", Path: t.TempDir() + "/weg.pdf"}}},
	})
	if err == nil || ExitCodeFor(err) == 8 {
		t.Fatalf("lokaler Fehler (nicht Exit 8) erwartet, got %v", err)
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Error("ohne Datei kein Request")
	}
}

// TestRateLimitOnlyRetriesJust429: Ein 429 beweist, dass nichts passiert
// ist — ein 5xx oder Netzwerkfehler nicht. Für Aufrufe, die eine Analyse
// (und damit KI-Kontingent) auslösen, gilt deshalb nur 429 als wiederholbar.
func TestRateLimitOnlyRetriesJust429(t *testing.T) {
	srv, calls := scripted(t, []int{429, 202}, nil)
	var sleeps []time.Duration
	if _, err := retryClient(srv, &sleeps).Do(Request{Method: "POST", Path: "/api/x", Body: []byte(`{}`), RetryRateLimit: true}); err != nil {
		t.Fatalf("429 soll wiederholt werden: %v", err)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Errorf("zwei Versuche erwartet, got %d", *calls)
	}
	for _, status := range []int{502, 503, 504} {
		srv, calls := scripted(t, []int{status, 202}, nil)
		if _, err := retryClient(srv, &sleeps).Do(Request{Method: "POST", Path: "/api/x", Body: []byte(`{}`), RetryRateLimit: true}); err == nil {
			t.Fatalf("%d: Fehler ohne Wiederholung erwartet", status)
		}
		if atomic.LoadInt32(calls) != 1 {
			t.Errorf("%d: genau ein Versuch erwartet, got %d", status, *calls)
		}
	}
}

// slowFirst antwortet beim ersten Aufruf zu spät, danach sofort.
func slowFirst(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv, &calls
}

// TestTimeoutIsNeverRetriedForWrites: Ein Timeout sagt nicht, ob der Aufruf
// angekommen ist — und eine Wiederholung hebelte --timeout aus. Auch ein
// idempotenter Schreibaufruf läuft deshalb nach einem Timeout nicht noch mal.
func TestTimeoutIsNeverRetriedForWrites(t *testing.T) {
	srv, calls := slowFirst(t)
	var sleeps []time.Duration
	c := retryClient(srv, &sleeps)
	c.Timeout = 50 * time.Millisecond
	_, err := c.Do(Request{Method: "POST", Path: "/api/x", Body: []byte(`{}`), Idempotent: true})
	if err == nil {
		t.Fatal("Timeout-Fehler erwartet")
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("keine Wiederholung nach Timeout erwartet, got %d Versuche", *calls)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "TIMEOUT" || ExitCodeFor(err) != 8 {
		t.Errorf("Code TIMEOUT mit Exit 8 erwartet, got %#v", err)
	}
}

// TestTimeoutIsRetriedForReads: Lesen darf nach einem Timeout erneut
// versuchen — --timeout gilt je Versuch.
func TestTimeoutIsRetriedForReads(t *testing.T) {
	srv, calls := slowFirst(t)
	var sleeps []time.Duration
	c := retryClient(srv, &sleeps)
	c.Timeout = 50 * time.Millisecond
	if _, err := c.Do(Request{Method: "GET", Path: "/api/x"}); err != nil {
		t.Fatalf("Erfolg im zweiten Versuch erwartet, got %v", err)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Errorf("zwei Versuche erwartet, got %d", *calls)
	}
}
