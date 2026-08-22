package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/immoJUMP/immojump-cli/internal/config"
)

func loadConfig(t *testing.T, h *harness) *config.File {
	t.Helper()
	file, err := config.Load(h.env["IMMOJUMP_CONFIG"])
	if err != nil {
		t.Fatalf("Config laden: %v", err)
	}
	return file
}

func TestAuthLoginWritesContextAndVerifies(t *testing.T) {
	h := newHarness(t)
	// Ohne Env-Token/Org: login liefert alles per Flag.
	delete(h.env, "IMMOJUMP_TOKEN")
	delete(h.env, "IMMOJUMP_ORGANISATION_ID")
	h.respond = `{"id":"u1","username":"chris@immojump.de"}`

	code, stdout, stderr := h.run("auth", "login",
		"--context", "beta",
		"--base-url", h.server.URL,
		"--organisation", "org-9",
		"--token", "tok-9")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last == nil || h.last.Path != "/api/user/me" {
		t.Fatalf("Prüfung gegen /api/user/me erwartet, got %+v", h.last)
	}
	if h.last.Header.Get("Authorization") != "Bearer tok-9" {
		t.Errorf("Token aus dem Flag erwartet, got %q", h.last.Header.Get("Authorization"))
	}
	if h.last.Header.Get("X-Organisation-Id") != "org-9" {
		t.Errorf("Organisation aus dem Flag erwartet, got %q", h.last.Header.Get("X-Organisation-Id"))
	}

	file := loadConfig(t, h)
	if file.CurrentContext != "beta" {
		t.Errorf("current_context beta erwartet, got %q", file.CurrentContext)
	}
	ctx := file.Contexts["beta"]
	if ctx.Token != "tok-9" || ctx.OrganisationID != "org-9" || ctx.BaseURL != h.server.URL {
		t.Errorf("Context falsch gespeichert: %+v", ctx)
	}

	if !strings.Contains(stdout, "beta") {
		t.Errorf("Bestätigung mit Context-Namen erwartet, got %q", stdout)
	}
	if strings.Contains(stdout, "tok-9") {
		t.Errorf("Token darf nicht im Klartext ausgegeben werden: %q", stdout)
	}
}

func TestAuthLoginWithTokenEnvStoresReferenceOnly(t *testing.T) {
	h := newHarness(t)
	h.env["MY_TOKEN"] = "tok-aus-env"
	code, _, stderr := h.run("auth", "login",
		"--context", "prod",
		"--base-url", h.server.URL,
		"--organisation", "org-1",
		"--token-env", "MY_TOKEN")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	ctx := loadConfig(t, h).Contexts["prod"]
	if ctx.TokenEnv != "MY_TOKEN" {
		t.Errorf("token_env erwartet, got %+v", ctx)
	}
	if ctx.Token != "" {
		t.Errorf("kein Klartext-Token erwartet, got %q", ctx.Token)
	}
	if h.last.Header.Get("Authorization") != "Bearer tok-aus-env" {
		t.Errorf("Token aus der Env-Variablen erwartet, got %q", h.last.Header.Get("Authorization"))
	}
}

// TestAuthLoginTokenEnvIsStrict: Wer --token-env sagt, meint genau diese
// Variable. Ein stiller Rückfall auf IMMOJUMP_TOKEN würde einen Context
// speichern, der auf eine leere Variable zeigt — und der später wortlos
// ein fremdes Token benutzt.
func TestAuthLoginTokenEnvIsStrict(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_TOKEN"] = "tok-aus-der-umgebung"
	code, _, stderr := h.run("auth", "login",
		"--context", "prod",
		"--base-url", h.server.URL,
		"--organisation", "org-1",
		"--token-env", "FEHLT_KOMPLETT")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("ohne Token darf keine Prüfung rausgehen")
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(msg, "FEHLT_KOMPLETT") {
		t.Errorf("Meldung soll die Variable benennen, got %q", msg)
	}
	if _, err := os.Stat(h.env["IMMOJUMP_CONFIG"]); err == nil {
		if _, ok := loadConfig(t, h).Contexts["prod"]; ok {
			t.Error("nichts speichern, wenn die Variable leer ist")
		}
	}
}

func TestAuthLoginTokenEnvEmptyValueIsAlsoStrict(t *testing.T) {
	h := newHarness(t)
	h.env["LEER"] = ""
	h.env["IMMOJUMP_TOKEN"] = "tok-aus-der-umgebung"
	if code, _, stderr := h.run("auth", "login", "--context", "prod",
		"--base-url", h.server.URL, "--token-env", "LEER"); code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
}

// TestAuthLoginWithoutConfigPathExitsWith3: Ohne HOME/XDG/IMMOJUMP_CONFIG
// gibt es keinen Ort für die Token-Datei — dann lieber sauber abbrechen als
// ins Arbeitsverzeichnis schreiben.
func TestAuthLoginWithoutConfigPathExitsWith3(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_CONFIG")
	code, _, stderr := h.run("auth", "login", "--context", "x",
		"--base-url", h.server.URL, "--token", "tok")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(msg, "IMMOJUMP_CONFIG") {
		t.Errorf("Meldung soll IMMOJUMP_CONFIG nennen, got %q", msg)
	}
	if h.last != nil {
		t.Error("ohne Speicherort gar nicht erst prüfen")
	}
}

func TestAuthLoginFailsOnUnauthorized(t *testing.T) {
	h := newHarness(t)
	h.status = 401
	h.respond = `{"message":"Token ungültig"}`
	code, _, stderr := h.run("auth", "login",
		"--context", "kaputt", "--base-url", h.server.URL, "--organisation", "o", "--token", "falsch")
	if code != 4 {
		t.Fatalf("Exit 4 erwartet, got %d", code)
	}
	if errorLine(t, stderr)["message"] != "Token ungültig" {
		t.Error("Backend-Meldung erwartet")
	}
	if _, err := os.Stat(h.env["IMMOJUMP_CONFIG"]); err == nil {
		file := loadConfig(t, h)
		if _, ok := file.Contexts["kaputt"]; ok {
			t.Error("ein nicht verifizierter Context darf nicht gespeichert werden")
		}
	}
}

func TestAuthLoginRequiresToken(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	code, _, stderr := h.run("auth", "login", "--context", "x", "--base-url", h.server.URL)
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
}

func TestAuthStatusShowsResolvedConfigWithMaskedToken(t *testing.T) {
	h := newHarness(t)
	h.env["IMMOJUMP_TOKEN"] = "abcdefghijklmnop"
	h.respond = `{"username":"chris@immojump.de"}`

	code, stdout, stderr := h.run("auth", "status")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("JSON auf stdout erwartet: %q (%v)", stdout, err)
	}
	if out["base_url"] != h.server.URL {
		t.Errorf("aufgelöste Base-URL erwartet, got %#v", out["base_url"])
	}
	if out["organisation_id"] != "org-test" {
		t.Errorf("aufgelöste Organisation erwartet, got %#v", out["organisation_id"])
	}
	token, _ := out["token"].(string)
	if token == "abcdefghijklmnop" || !strings.Contains(token, "*") {
		t.Errorf("maskiertes Token erwartet, got %q", token)
	}
	if out["user"] == nil {
		t.Error("Antwort von /api/user/me erwartet")
	}
	if strings.Contains(stdout, "abcdefghijklmnop") {
		t.Error("Klartext-Token darf nirgends auftauchen")
	}
}

func TestAuthStatusWithoutTokenExitsWith3(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "IMMOJUMP_TOKEN")
	if code, _, _ := h.run("auth", "status"); code != 3 {
		t.Fatal("Exit 3 ohne Token erwartet")
	}
}

func TestContextLifecycle(t *testing.T) {
	h := newHarness(t)

	// Anlegen über zwei Logins.
	for _, name := range []string{"prod", "beta"} {
		if code, _, stderr := h.run("auth", "login", "--context", name,
			"--base-url", h.server.URL, "--organisation", "org-"+name, "--token", "tok-"+name); code != 0 {
			t.Fatalf("login %s: Exit 0 erwartet, got %d (%s)", name, code, stderr)
		}
	}

	code, stdout, _ := h.run("context", "list")
	if code != 0 {
		t.Fatalf("context list: Exit 0 erwartet, got %d", code)
	}
	var listed struct {
		CurrentContext string `json:"current_context"`
		Contexts       []struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			Current bool   `json:"current"`
		} `json:"contexts"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("context list soll JSON liefern: %q (%v)", stdout, err)
	}
	if len(listed.Contexts) != 2 {
		t.Fatalf("zwei Contexts erwartet, got %+v", listed.Contexts)
	}
	if listed.CurrentContext != "beta" {
		t.Errorf("letzter Login ist current, got %q", listed.CurrentContext)
	}

	// Wechseln.
	if code, _, stderr := h.run("context", "use", "prod"); code != 0 {
		t.Fatalf("context use: Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	code, stdout, _ = h.run("context", "current")
	if code != 0 {
		t.Fatalf("context current: Exit 0 erwartet, got %d", code)
	}
	if !strings.Contains(stdout, "prod") {
		t.Errorf("prod als aktueller Context erwartet, got %q", stdout)
	}

	// Der aktive Context steuert jetzt die Requests.
	if code, _, stderr := h.run("contacts", "list"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	// Env schlägt die Datei — deshalb hier ohne Env-Token/Org prüfen.
	h2 := newHarness(t)
	h2.env["IMMOJUMP_CONFIG"] = h.env["IMMOJUMP_CONFIG"]
	delete(h2.env, "IMMOJUMP_TOKEN")
	delete(h2.env, "IMMOJUMP_ORGANISATION_ID")
	delete(h2.env, "IMMOJUMP_BASE_URL")
	h2.env["IMMOJUMP_EXTRA_BASE_URLS"] = h.server.URL
	if code, _, stderr := h2.run("contacts", "list"); code != 0 {
		t.Fatalf("Context als Quelle: Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Header.Get("Authorization") != "Bearer tok-prod" {
		t.Errorf("Token aus dem Context prod erwartet, got %q", h.last.Header.Get("Authorization"))
	}

	// Löschen.
	if code, _, stderr := h.run("context", "delete", "beta"); code != 0 {
		t.Fatalf("context delete: Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	file := loadConfig(t, h)
	if _, ok := file.Contexts["beta"]; ok {
		t.Error("beta sollte gelöscht sein")
	}
	if file.CurrentContext != "prod" {
		t.Errorf("current_context bleibt prod, got %q", file.CurrentContext)
	}
}

func TestContextDeleteCurrentClearsCurrent(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("auth", "login", "--context", "nur-einer",
		"--base-url", h.server.URL, "--organisation", "o", "--token", "t"); code != 0 {
		t.Fatalf("login: %d (%s)", code, stderr)
	}
	if code, _, stderr := h.run("context", "delete", "nur-einer"); code != 0 {
		t.Fatalf("delete: %d (%s)", code, stderr)
	}
	if got := loadConfig(t, h).CurrentContext; got != "" {
		t.Errorf("current_context muss geleert werden, got %q", got)
	}
}

func TestContextUseUnknownExitsWith3(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("context", "use", "gibtsnicht")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	if errorLine(t, stderr)["message"] == "" {
		t.Error("erklärende Meldung erwartet")
	}
}

func TestUnknownContextFlagExitsWith3(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.run("--context", "gibtsnicht", "contacts", "list")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d", code)
	}
}

func TestContextListOnEmptyConfig(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("context", "list")
	if code != 0 {
		t.Fatalf("Exit 0 auch ohne Config erwartet, got %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "contexts") {
		t.Errorf("leere Liste als JSON erwartet, got %q", stdout)
	}
}

func TestContextCommandsDoNotCallTheAPI(t *testing.T) {
	h := newHarness(t)
	h.run("context", "list")
	h.run("context", "current")
	if h.last != nil {
		t.Error("context-Befehle sind rein lokal")
	}
}
