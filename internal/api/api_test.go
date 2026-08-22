package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testClient baut einen Client, der auf den httptest-Server zeigen darf.
func testClient(srv *httptest.Server) *Client {
	return &Client{
		BaseURL: srv.URL,
		Org:     "org-1",
		Token:   "tok-1",
		Env: func(key string) string {
			if key == "IMMOJUMP_EXTRA_BASE_URLS" {
				return srv.URL
			}
			return ""
		},
	}
}

func TestDoSetsAuthHeaders(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	res, err := testClient(srv).Do(Request{Method: "GET", Path: "/api/contacts"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.Status != 200 {
		t.Errorf("Status 200 erwartet, got %d", res.Status)
	}
	if h := got.Header.Get("Authorization"); h != "Bearer tok-1" {
		t.Errorf("Bearer-Header erwartet, got %q", h)
	}
	if h := got.Header.Get("X-Organisation-Id"); h != "org-1" {
		t.Errorf("X-Organisation-Id erwartet, got %q", h)
	}
	if got.URL.Path != "/api/contacts" {
		t.Errorf("Pfad /api/contacts erwartet, got %q", got.URL.Path)
	}
	if ua := got.Header.Get("User-Agent"); !strings.Contains(ua, "immojump-cli") {
		t.Errorf("User-Agent immojump-cli erwartet, got %q", ua)
	}
}

func TestDoOmitsOrgHeaderWhenUnknown(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.Org = ""
	if _, err := c.Do(Request{Method: "GET", Path: "/api/user/me"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if h := got.Header.Get("X-Organisation-Id"); h != "" {
		t.Errorf("ohne Organisation kein Header erwartet, got %q", h)
	}
}

func TestDoSendsJSONBodyAndIdempotencyKey(t *testing.T) {
	var body string
	var contentType, idem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		contentType = r.Header.Get("Content-Type")
		idem = r.Header.Get("Idempotency-Key")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	res, err := testClient(srv).Do(Request{
		Method:         "POST",
		Path:           "/api/contacts",
		Body:           []byte(`{"vorname":"Ada"}`),
		ContentType:    "application/json",
		IdempotencyKey: "key-42",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.Status != 201 {
		t.Errorf("Status 201 erwartet, got %d", res.Status)
	}
	if body != `{"vorname":"Ada"}` {
		t.Errorf("Body durchgereicht erwartet, got %q", body)
	}
	if contentType != "application/json" {
		t.Errorf("application/json erwartet, got %q", contentType)
	}
	if idem != "key-42" {
		t.Errorf("Idempotency-Key erwartet, got %q", idem)
	}
}

func TestDoMergesQuery(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	q := url.Values{}
	q.Set("for", "contact")
	q.Add("tag", "a")
	q.Add("tag", "b")
	if _, err := testClient(srv).Do(Request{Method: "GET", Path: "/api/org-1/tags", Query: q}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if query.Get("for") != "contact" {
		t.Errorf("for=contact erwartet, got %q", query.Get("for"))
	}
	if len(query["tag"]) != 2 {
		t.Errorf("zwei tag-Werte erwartet, got %v", query["tag"])
	}
}

func TestErrorUsesBackendMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"message":"Kein Zugriff auf diese Organisation","code":"ORG_FORBIDDEN"}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).Do(Request{Method: "GET", Path: "/api/contacts"})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("*api.Error erwartet, got %T", err)
	}
	if apiErr.Message != "Kein Zugriff auf diese Organisation" {
		t.Errorf("Backend-Message unverändert erwartet, got %q", apiErr.Message)
	}
	if apiErr.Code != "ORG_FORBIDDEN" {
		t.Errorf("Code erwartet, got %q", apiErr.Code)
	}
	if apiErr.ExitCode() != 6 {
		t.Errorf("Exit 6 erwartet, got %d", apiErr.ExitCode())
	}
}

func TestErrorFallsBackToErrorKeyAndRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`<html>kaputt</html>`))
	}))
	defer srv.Close()

	_, err := testClient(srv).Do(Request{Method: "GET", Path: "/api/contacts"})
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("*api.Error erwartet, got %T (%v)", err, err)
	}
	if apiErr.Message == "" {
		t.Error("auch ohne JSON-Body eine Meldung erwartet")
	}
	if apiErr.ExitCode() != 8 {
		t.Errorf("Exit 8 erwartet, got %d", apiErr.ExitCode())
	}
}

func TestErrorKeyVariant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"Feld kaufpreis fehlt"}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).Do(Request{Method: "POST", Path: "/api/v2/immobilien"})
	apiErr := err.(*Error)
	if apiErr.Message != "Feld kaufpreis fehlt" {
		t.Errorf("error-Key als Meldung erwartet, got %q", apiErr.Message)
	}
	if apiErr.ExitCode() != 11 {
		t.Errorf("Exit 11 erwartet, got %d", apiErr.ExitCode())
	}
}

func TestExitCodeMapping(t *testing.T) {
	cases := map[int]int{
		400: 11, 401: 4, 403: 6, 404: 5, 409: 9,
		422: 11, 429: 7, 500: 8, 502: 8, 503: 8, 402: 1, 418: 1,
	}
	for status, want := range cases {
		e := &Error{Status: status}
		if got := e.ExitCode(); got != want {
			t.Errorf("Status %d: Exit %d erwartet, got %d", status, want, got)
		}
	}
}

func TestNonJSONResponseIsPassedThroughRaw(t *testing.T) {
	yaml := "name: Ankauf\nstatuses:\n  - Neu\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte(yaml))
	}))
	defer srv.Close()

	res, err := testClient(srv).Do(Request{Method: "GET", Path: "/api/pipelines/pipelines/1/export"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(res.Body) != yaml {
		t.Errorf("YAML unverändert erwartet, got %q", res.Body)
	}
	if res.ContentType != "application/x-yaml" {
		t.Errorf("Content-Type durchgereicht erwartet, got %q", res.ContentType)
	}
}

func TestNetworkErrorMapsToExit8(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // Server ist weg -> Verbindungsfehler

	c := &Client{
		BaseURL: url,
		Token:   "t",
		Env:     func(k string) string { return url },
	}
	_, err := c.Do(Request{Method: "GET", Path: "/api/contacts"})
	if err == nil {
		t.Fatal("Netzwerkfehler erwartet")
	}
	if ExitCodeFor(err) != 8 {
		t.Errorf("Exit 8 für Netzwerkfehler erwartet, got %d", ExitCodeFor(err))
	}
}

func TestMultipartUpload(t *testing.T) {
	var (
		filename, content string
		org, immo, dup    string
		fieldName         string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		for name, headers := range r.MultipartForm.File {
			fieldName = name
			if len(headers) > 0 {
				filename = headers[0].Filename
				f, _ := headers[0].Open()
				raw, _ := io.ReadAll(f)
				content = string(raw)
			}
		}
		org = r.FormValue("organisation_id")
		immo = r.FormValue("immobilien_id")
		dup = r.FormValue("allow_duplicate_upload")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uploaded":1}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).Do(Request{
		Method: "POST",
		Path:   "/api/documents/documents/bulk-upload",
		Multipart: &Multipart{
			Files:  []FilePart{{Field: "files[]", Filename: "expose.pdf", Content: []byte("%PDF-1.4")}},
			Fields: map[string]string{"organisation_id": "org-1", "immobilien_id": "42", "allow_duplicate_upload": "true"},
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if fieldName != "files[]" {
		t.Errorf("Feldname files[] erwartet, got %q", fieldName)
	}
	if filename != "expose.pdf" || content != "%PDF-1.4" {
		t.Errorf("Datei falsch übertragen: %q / %q", filename, content)
	}
	if org != "org-1" || immo != "42" || dup != "true" {
		t.Errorf("Formfelder falsch: org=%q immo=%q dup=%q", org, immo, dup)
	}
}

func TestAllowlistBlocksUnknownHost(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "t", Env: func(string) string { return "" }}
	_, err := c.Do(Request{Method: "GET", Path: "/api/contacts"})
	if err == nil {
		t.Fatal("Allowlist-Ablehnung erwartet")
	}
	if called {
		t.Error("es hätte gar kein Request rausgehen dürfen")
	}
	if ExitCodeFor(err) != 3 {
		t.Errorf("Exit 3 (lokale Konfiguration) erwartet, got %d", ExitCodeFor(err))
	}
}

// TestTimeoutAppliesToInjectedClient: Der Timeout ist eine Zusage des CLI,
// kein Detail des HTTP-Clients. Ein injizierter Client (Tests, künftige
// Aufrufer) darf sie nicht aushebeln — sonst hängt ein Agent unbegrenzt.
func TestTimeoutAppliesToInjectedClient(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(done)

	c := testClient(srv)
	c.HTTP = &http.Client{} // ohne eigenen Timeout
	c.Timeout = 50 * time.Millisecond

	start := time.Now()
	_, err := c.Do(Request{Method: "GET", Path: "/api/contacts"})
	if err == nil {
		t.Fatal("Timeout-Fehler erwartet")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Abbruch nach dem Timeout erwartet, dauerte %v", elapsed)
	}
	if got := ExitCodeFor(err); got != 8 {
		t.Errorf("Exit 8 (temporär, Retry möglich) erwartet, got %d", got)
	}
}

func TestExitCodeForPlainError(t *testing.T) {
	if got := ExitCodeFor(io.EOF); got != 1 {
		t.Errorf("unbekannter Fehler -> Exit 1 erwartet, got %d", got)
	}
	if got := ExitCodeFor(nil); got != 0 {
		t.Errorf("kein Fehler -> Exit 0 erwartet, got %d", got)
	}
}
