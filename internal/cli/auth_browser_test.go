package cli

import (
	"strings"
	"testing"
)

func TestTokenPageURL(t *testing.T) {
	cases := map[string]string{
		"https://immojump.de":       "https://immojump.de/settings/api-access",
		"https://beta.immojump.de/": "https://beta.immojump.de/settings/api-access",
		"":                          "",
	}
	for base, want := range cases {
		if got := tokenPageURL(base); got != want {
			t.Errorf("tokenPageURL(%q) = %q, want %q", base, got, want)
		}
	}
}

// Der Kern des Flows: Wer keinen Token als Flag hat, bekommt ihn über stdin
// hinein — `immojump auth login < token.txt` oder interaktiv nach dem
// Browser-Hinweis. Vorher endete derselbe Aufruf mit Exit 3.
func TestAuthLoginReadsTokenFromStdin(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	h.stdin = "tok-aus-stdin\n"
	h.respond = `{"id":1,"username":"chris@example.com"}`

	code, stdout, stderr := h.run("auth", "login")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last == nil {
		t.Fatal("Der Token muss gegen die Instanz geprüft werden")
	}
	if got := h.last.Header.Get("Authorization"); got != "Bearer tok-aus-stdin" {
		t.Errorf("Token aus stdin erwartet, got %q", got)
	}
	if !strings.Contains(stdout, "context") {
		t.Errorf("gespeicherten Context erwartet, got %q", stdout)
	}
}

// Whitespace und ein versehentlich mitkopiertes "Bearer " dürfen den Login
// nicht kaputtmachen — beides passiert beim Kopieren aus der Web-App.
func TestAuthLoginCleansPastedToken(t *testing.T) {
	for _, pasted := range []string{"  tok-sauber  \n", "Bearer tok-sauber\n", "tok-sauber"} {
		h := newHarness(t)
		delete(h.env, "IMMOJUMP_TOKEN")
		h.stdin = pasted
		h.respond = `{"id":1}`
		if code, _, stderr := h.run("auth", "login"); code != 0 {
			t.Fatalf("%q: Exit 0 erwartet, got %d (%s)", pasted, code, stderr)
		}
		if got := h.last.Header.Get("Authorization"); got != "Bearer tok-sauber" {
			t.Errorf("%q: bereinigten Token erwartet, got %q", pasted, got)
		}
	}
}

// Leere Eingabe darf keinen leeren Context speichern.
func TestAuthLoginRejectsEmptyStdin(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	h.stdin = "\n"
	code, _, stderr := h.run("auth", "login")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("ohne Token darf kein Request rausgehen")
	}
	// Die Meldung muss sagen, WO der Token herkommt.
	if line := errorLine(t, stderr); !strings.Contains(line["message"].(string), "/settings/api-access") {
		t.Errorf("Meldung soll die Token-Seite nennen: %v", line["message"])
	}
}

// --no-browser gehört zum Befehl, damit der Flow auf Servern ohne Browser
// nutzbar bleibt.
func TestAuthLoginHasNoBrowserFlag(t *testing.T) {
	spec, ok := Lookup("auth", "login")
	if !ok {
		t.Fatal("auth login fehlt")
	}
	found := false
	for _, flag := range spec.Flags {
		if flag.Name == "no-browser" {
			found = true
			if flag.Kind != FlagBool {
				t.Errorf("--no-browser soll ein Schalter sein, ist %q", flag.Kind)
			}
		}
	}
	if !found {
		t.Error("--no-browser fehlt bei auth login")
	}
}

// --token schlägt stdin: Wer ihn explizit angibt, meint ihn.
func TestAuthLoginPrefersExplicitToken(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	h.stdin = "tok-aus-stdin\n"
	h.respond = `{"id":1}`
	if code, _, stderr := h.run("auth", "login", "--token", "tok-explizit"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if got := h.last.Header.Get("Authorization"); got != "Bearer tok-explizit" {
		t.Errorf("--token soll gewinnen, got %q", got)
	}
}
