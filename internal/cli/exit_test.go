package cli

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPStatusToExitCode(t *testing.T) {
	cases := []struct {
		status int
		want   int
	}{
		{400, 11}, {401, 4}, {403, 6}, {404, 5}, {409, 9},
		{422, 11}, {429, 7}, {500, 8}, {503, 8}, {402, 1},
	}
	for _, tc := range cases {
		h := newHarness(t)
		h.status = tc.status
		h.respond = `{"message":"Backend sagt nein"}`
		code, stdout, stderr := h.run("contacts", "list")
		if code != tc.want {
			t.Errorf("Status %d: Exit %d erwartet, got %d", tc.status, tc.want, code)
		}
		if stdout != "" {
			t.Errorf("Status %d: kein stdout bei Fehlern erwartet, got %q", tc.status, stdout)
		}
		line := errorLine(t, stderr)
		if line["message"] != "Backend sagt nein" {
			t.Errorf("Status %d: Backend-Meldung unverändert erwartet, got %#v", tc.status, line["message"])
		}
		if line["error"] != true {
			t.Errorf("Status %d: error:true erwartet", tc.status)
		}
		if int(line["status"].(float64)) != tc.status {
			t.Errorf("Status %d: status im Fehler-JSON erwartet, got %#v", tc.status, line["status"])
		}
	}
}

func TestNetworkErrorExitsWith8(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed := srv.URL
	srv.Close()

	h := newHarness(t)
	h.env["IMMOJUMP_BASE_URL"] = closed
	h.env["IMMOJUMP_EXTRA_BASE_URLS"] = closed
	code, _, stderr := h.run("contacts", "list")
	if code != 8 {
		t.Fatalf("Exit 8 erwartet, got %d (%s)", code, stderr)
	}
}

func TestMissingTokenExitsWith3(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	code, _, stderr := h.run("contacts", "list")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d", code)
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "token") {
		t.Errorf("Meldung soll das fehlende Token benennen, got %q", msg)
	}
	if h.last != nil {
		t.Error("ohne Token darf kein Request rausgehen")
	}
}

func TestMissingOrgOnOrgPathExitsWith3(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_ORGANISATION_ID")
	code, _, stderr := h.run("pipelines", "list")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "organisation") {
		t.Errorf("Meldung soll die fehlende Organisation benennen, got %q", msg)
	}
}

func TestMissingOrgIsFineForHeaderOnlyRoutes(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_ORGANISATION_ID")
	if code, _, stderr := h.run("contacts", "list"); code != 0 {
		t.Fatalf("ohne {org} im Pfad reicht der Header, Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Header.Get("X-Organisation-Id") != "" {
		t.Error("ohne Organisation kein Header")
	}
}

func TestUsageErrorsExitWith2(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unbekannte Ressource", []string{"kontakte", "list"}},
		{"unbekannter Verb", []string{"contacts", "loeschen", "1"}},
		{"fehlendes Arg", []string{"contacts", "get"}},
		{"zu viele Args", []string{"contacts", "get", "1", "2"}},
		{"unbekanntes Flag", []string{"contacts", "list", "--gibtsnicht"}},
		{"Flag ohne Wert", []string{"contacts", "list", "--fields"}},
		{"kaputtes --body", []string{"contacts", "create", "--body", "{kein json"}},
		{"kaputtes --set", []string{"contacts", "create", "--set", "ohnegleich"}},
		{"kaputtes -q", []string{"contacts", "list", "-q", "ohnegleich"}},
		{"fehlender Verb", []string{"contacts"}},
		{"api ohne Pfad", []string{"api", "GET"}},
		{"documents rename ohne --name", []string{"documents", "rename", "11"}},
		{"tags set ohne --tag-ids", []string{"tags", "set", "contact", "42"}},
		{"api mit Nicht-API-Pfad", []string{"api", "GET", "/login"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.args...)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (stderr: %s)", code, stderr)
			}
			if errorLine(t, stderr)["message"] == "" {
				t.Error("erklärende Meldung erwartet")
			}
		})
	}
}

// TestSpecialCommandsRejectGlobalBodyFlags: Befehle, die ihren Body selbst
// bauen, dürfen --set/--body nicht stillschweigend schlucken — sonst denkt
// ein Agent, sein Feld sei angekommen.
func TestSpecialCommandsRejectGlobalBodyFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"documents upload --set", []string{"documents", "upload", "x.pdf", "--set", "a=b"}},
		{"documents upload --body", []string{"documents", "upload", "x.pdf", "--body", `{"a":1}`}},
		{"pipelines import --set", []string{"pipelines", "import", "--set", "a=b"}},
		{"pipelines import --body", []string{"pipelines", "import", "--body", `{"a":1}`}},
		{"tags set --set", []string{"tags", "set", "contact", "42", "--tag-ids", "1", "--set", "a=b"}},
		{"tags set --body", []string{"tags", "set", "contact", "42", "--tag-ids", "1", "--body", `[1]`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.args...)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("kein Request bei Bedienfehler")
			}
			msg, _ := errorLine(t, stderr)["message"].(string)
			if !strings.Contains(msg, "--set") && !strings.Contains(msg, "--body") {
				t.Errorf("Meldung soll die abgelehnten Flags benennen, got %q", msg)
			}
		})
	}
}

// TestAuthAndContextRejectRequestFlags: auth und context bauen ihren Request
// vollständig selbst — -q/--set/--body hätten dort keine Wirkung.
func TestAuthAndContextRejectRequestFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"auth login -q", []string{"auth", "login", "--token", "t", "-q", "a=b"}},
		{"auth login --set", []string{"auth", "login", "--token", "t", "--set", "a=b"}},
		{"auth login --body", []string{"auth", "login", "--token", "t", "--body", `{"a":1}`}},
		{"auth status -q", []string{"auth", "status", "-q", "a=b"}},
		{"auth status --set", []string{"auth", "status", "--set", "a=b"}},
		{"auth status --body", []string{"auth", "status", "--body", `{"a":1}`}},
		{"context list -q", []string{"context", "list", "-q", "a=b"}},
		{"context current --set", []string{"context", "current", "--set", "a=b"}},
		{"context use --body", []string{"context", "use", "x", "--body", `{"a":1}`}},
		{"context delete -q", []string{"context", "delete", "x", "-q", "a=b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.args...)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("kein Request bei Bedienfehler")
			}
			if errorLine(t, stderr)["message"] == "" {
				t.Error("erklärende Meldung erwartet")
			}
		})
	}
}

// failWriter simuliert ein kaputtes stdout (volle Platte, geschlossene Pipe).
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("stdout kaputt") }

// TestLocalWriteFailureExitsWith1: Exit 8 verspricht "temporär, Retry möglich"
// — ein lokaler Schreibfehler ist das nicht.
func TestLocalWriteFailureExitsWith1(t *testing.T) {
	h := newHarness(t)
	stderr := &bytes.Buffer{}
	code := Run([]string{"contacts", "list"}, Options{
		Stdout: failWriter{},
		Stderr: stderr,
		Getenv: func(key string) string { return h.env[key] },
	})
	if code != 1 {
		t.Fatalf("Exit 1 für lokalen Fehler erwartet, got %d (%s)", code, stderr.String())
	}
	if errorLine(t, stderr.String())["message"] == "" {
		t.Error("erklärende Meldung erwartet")
	}
}

func TestUnknownResourceSuggestsNearMatch(t *testing.T) {
	h := newHarness(t)
	_, _, stderr := h.run("contact", "list")
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(msg, "contacts") {
		t.Errorf("Vorschlag contacts erwartet, got %q", msg)
	}
}

func TestUnknownVerbSuggestsNearMatch(t *testing.T) {
	h := newHarness(t)
	_, _, stderr := h.run("contacts", "gett", "1")
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(msg, "get") {
		t.Errorf("Vorschlag get erwartet, got %q", msg)
	}
}

func TestBlockedBaseURLExitsWith3(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_BASE_URL"] = "https://immojump.evil.example.com"
	delete(h.env, "IMMOJUMP_EXTRA_BASE_URLS")
	code, _, stderr := h.run("contacts", "list")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("an eine nicht erlaubte Instanz darf nichts gehen")
	}
}

func TestSuccessWritesNothingToStderr(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("contacts", "list")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if stderr != "" {
		t.Errorf("stderr soll bei Erfolg leer bleiben, got %q", stderr)
	}
	if stdout != `{"ok":true}`+"\n" {
		t.Errorf("Nutzdaten auf stdout erwartet, got %q", stdout)
	}
}
