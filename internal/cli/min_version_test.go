package cli

import (
	"net/http"
	"strings"
	"testing"
)

const minVersionHeader = "X-Immojump-Cli-Min-Version"

func withMinVersion(status int, body, minimum string) func(*recorded) reply {
	return func(*recorded) reply {
		return reply{status: status, body: body, header: http.Header{minVersionHeader: {minimum}}}
	}
}

// TestUserAgentCarriesVersion: Das Backend sieht, welche CLI-Version anfragt
// — Voraussetzung, um alte Versionen gezielt anzusprechen.
func TestUserAgentCarriesVersion(t *testing.T) {
	h := newHarness(t)
	h.version = "v0.10.0"
	if code, _, stderr := h.run("contacts", "get", "1"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if ua := h.last.Header.Get("User-Agent"); ua != "immojump-cli/v0.10.0" {
		t.Errorf("User-Agent immojump-cli/v0.10.0 erwartet, got %q", ua)
	}
}

// TestMinVersionWarning: Meldet der Server eine Mindestversion über der
// installierten, steht das auf stderr — bei jedem Aufruf und auch ohne
// Terminal. Genau dieser Hinweis erreicht Agenten.
func TestMinVersionWarning(t *testing.T) {
	h := newHarness(t)
	h.version = "v0.4.0"
	h.handle = withMinVersion(200, `{"id":1}`, "0.8.0")

	code, stdout, stderr := h.run("contacts", "get", "1")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if !sameJSON(t, stdout, `{"id":1}`) {
		t.Errorf("stdout verändert: %q", stdout)
	}
	warnings := warningLines(t, stderr)
	if len(warnings) != 1 {
		t.Fatalf("genau eine Warnzeile erwartet, stderr %q", stderr)
	}
	w := warnings[0]
	msg, _ := w["message"].(string)
	if !strings.Contains(msg, "0.8.0") || !strings.Contains(msg, "immojump update") {
		t.Errorf("Hinweis nennt Mindestversion und Befehl nicht: %q", msg)
	}
	if w["installed"] != "v0.4.0" || w["minimum"] != "0.8.0" || w["update_command"] != "immojump update" {
		t.Errorf("Details fehlen: %v", w)
	}
}

// TestMinVersionWarningBeforeError: Scheitert der Aufruf, steht der Hinweis
// vor der Fehlerzeile — die Fehlerzeile bleibt die letzte, auf die Agenten
// schauen, und der Exit-Code bleibt der des Fehlers.
func TestMinVersionWarningBeforeError(t *testing.T) {
	h := newHarness(t)
	h.version = "v0.4.0"
	h.handle = withMinVersion(404, `{"message":"Kontakt nicht gefunden"}`, "0.8.0")

	code, _, stderr := h.run("contacts", "get", "1")
	if code != 5 {
		t.Fatalf("Exit 5 erwartet, got %d", code)
	}
	if len(warningLines(t, stderr)) != 1 {
		t.Errorf("Hinweis auf Mindestversion erwartet, stderr %q", stderr)
	}
	if got := errorLine(t, stderr)["message"]; got != "Kontakt nicht gefunden" {
		t.Errorf("Fehlerzeile muss die letzte sein, got %v", got)
	}
}

// TestMinVersionWarningOncePerCall: Mehrere Requests in einem Aufruf
// (Wiederholung, Blättern, Stapel) ergeben trotzdem nur einen Hinweis.
func TestMinVersionWarningOncePerCall(t *testing.T) {
	h := newHarness(t)
	h.version = "v0.4.0"
	calls := 0
	h.handle = func(*recorded) reply {
		calls++
		status := 200
		if calls == 1 {
			status = 503
		}
		return reply{status: status, body: `{"id":1}`, header: http.Header{minVersionHeader: {"0.8.0"}}}
	}
	code, _, stderr := h.run("contacts", "get", "1")
	if code != 0 || calls != 2 {
		t.Fatalf("Exit 0 nach zwei Requests (503, dann 200) erwartet, got Exit %d nach %d", code, calls)
	}
	count := 0
	for _, w := range warningLines(t, stderr) {
		if w["minimum"] != nil {
			count++
		}
	}
	if count != 1 {
		t.Errorf("genau ein Hinweis auf die Mindestversion erwartet, got %d (stderr %q)", count, stderr)
	}
}

// TestMinVersionSilent: Aktuelle Version, Entwicklungs-Build oder
// unlesbarer Header — kein Hinweis.
func TestMinVersionSilent(t *testing.T) {
	cases := map[string][2]string{
		"aktuell":           {"v0.10.0", "0.8.0"},
		"gleich":            {"v0.8.0", "0.8.0"},
		"Entwicklungsbuild": {"dev", "0.8.0"},
		"Header kaputt":     {"v0.4.0", "irgendwas"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.version = c[0]
			h.handle = withMinVersion(200, `{"id":1}`, c[1])
			if _, _, stderr := h.run("contacts", "get", "1"); stderr != "" {
				t.Errorf("stderr soll leer sein, got %q", stderr)
			}
		})
	}
}
