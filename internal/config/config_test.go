package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envMap liefert eine env-Funktion, die nur die gegebenen Schlüssel kennt.
func envMap(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestResolveEnvOnly(t *testing.T) {
	env := envMap(map[string]string{
		"IMMOJUMP_TOKEN":           "tok-env",
		"IMMOJUMP_ORGANISATION_ID": "org-env",
	})
	res, err := Resolve(&File{}, Overrides{}, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Token != "tok-env" || res.Org != "org-env" {
		t.Errorf("Token/Org aus Env erwartet, got %+v", res)
	}
	if res.BaseURL != "https://immojump.de" {
		t.Errorf("Default-Base-URL erwartet, got %q", res.BaseURL)
	}
}

func TestResolveContextFromFile(t *testing.T) {
	file := &File{
		CurrentContext: "beta",
		Contexts: map[string]Context{
			"beta": {
				BaseURL:        "https://beta.immojump.de/",
				OrganisationID: "org-beta",
				TokenEnv:       "MY_BETA_TOKEN",
			},
		},
	}
	env := envMap(map[string]string{"MY_BETA_TOKEN": "tok-beta"})
	res, err := Resolve(file, Overrides{}, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ContextName != "beta" {
		t.Errorf("ContextName beta erwartet, got %q", res.ContextName)
	}
	if res.BaseURL != "https://beta.immojump.de" {
		t.Errorf("Base-URL ohne Slash am Ende erwartet, got %q", res.BaseURL)
	}
	if res.Org != "org-beta" || res.Token != "tok-beta" {
		t.Errorf("Org/Token aus Context erwartet, got %+v", res)
	}
}

func TestResolveTokenEnvFallsBackToPlainToken(t *testing.T) {
	file := &File{
		CurrentContext: "p",
		Contexts: map[string]Context{
			"p": {Token: "tok-plain", TokenEnv: "UNSET_VAR"},
		},
	}
	res, err := Resolve(file, Overrides{}, envMap(nil))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Token != "tok-plain" {
		t.Errorf("Fallback auf token erwartet, got %q", res.Token)
	}
}

// TestResolveReportsTokenSource: "Warum nimmt der Aufruf ein anderes Token?"
// ist die häufigste Diagnosefrage — die Antwort gehört in die Auflösung,
// nicht in ein Rätsel.
func TestResolveReportsTokenSource(t *testing.T) {
	cases := []struct {
		name string
		file *File
		env  map[string]string
		want string
	}{
		{
			name: "Umgebungsvariable schlägt alles",
			file: &File{CurrentContext: "p", Contexts: map[string]Context{"p": {Token: "tok-datei"}}},
			env:  map[string]string{"IMMOJUMP_TOKEN": "tok-env"},
			want: "env:IMMOJUMP_TOKEN",
		},
		{
			name: "token_env des Contexts",
			file: &File{CurrentContext: "p", Contexts: map[string]Context{"p": {TokenEnv: "MY_TOKEN"}}},
			env:  map[string]string{"MY_TOKEN": "tok-indirekt"},
			want: "env:MY_TOKEN",
		},
		{
			name: "Klartext im Context",
			file: &File{CurrentContext: "p", Contexts: map[string]Context{"p": {Token: "tok-datei"}}},
			want: "context",
		},
		{
			name: "gar kein Token",
			file: &File{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Resolve(tc.file, Overrides{}, envMap(tc.env))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if res.TokenSource != tc.want {
				t.Errorf("TokenSource %q erwartet, got %q", tc.want, res.TokenSource)
			}
		})
	}
}

func TestResolvePrecedenceFlagOverEnvOverFile(t *testing.T) {
	file := &File{
		CurrentContext: "prod",
		Contexts: map[string]Context{
			"prod": {BaseURL: "https://immojump.de", OrganisationID: "org-file", Token: "tok-file"},
			"beta": {BaseURL: "https://beta.immojump.de", OrganisationID: "org-beta", Token: "tok-beta"},
		},
	}
	env := envMap(map[string]string{
		"IMMOJUMP_CONTEXT":         "prod",
		"IMMOJUMP_ORGANISATION_ID": "org-env",
	})
	// --context beta schlägt IMMOJUMP_CONTEXT=prod; --org schlägt Env und Datei.
	res, err := Resolve(file, Overrides{Context: "beta", Org: "org-flag"}, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ContextName != "beta" || res.Org != "org-flag" || res.Token != "tok-beta" {
		t.Errorf("Flag > Env > Datei verletzt: %+v", res)
	}

	// Ohne Flags: Env-Context und Env-Org gewinnen gegen die Datei.
	res, err = Resolve(file, Overrides{}, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ContextName != "prod" || res.Org != "org-env" || res.Token != "tok-file" {
		t.Errorf("Env > Datei verletzt: %+v", res)
	}
}

func TestResolveUnknownContextFails(t *testing.T) {
	_, err := Resolve(&File{}, Overrides{Context: "gibtsnicht"}, envMap(nil))
	if err == nil {
		t.Fatal("Fehler für unbekannten Context erwartet")
	}
}

func TestCheckBaseURLAllowlist(t *testing.T) {
	cases := []struct {
		url   string
		extra string
		ok    bool
	}{
		{"https://immojump.de", "", true},
		{"https://beta.immojump.de", "", true},
		{"http://localhost:8081", "", true},
		{"https://evil.example.com", "", false},
		{"https://opulibra-estate.de", "https://opulibra-estate.de", true},
		{"https://opulibra-estate.de/", "https://opulibra-estate.de", true},
		{"https://zweite.de", "https://erste.de,https://zweite.de", true},
	}
	for _, c := range cases {
		env := envMap(map[string]string{"IMMOJUMP_EXTRA_BASE_URLS": c.extra})
		err := CheckBaseURL(c.url, env)
		if c.ok && err != nil {
			t.Errorf("CheckBaseURL(%q) unerwartet abgelehnt: %v", c.url, err)
		}
		if !c.ok && err == nil {
			t.Errorf("CheckBaseURL(%q) hätte abgelehnt werden müssen", c.url)
		}
	}
}

func TestCheckBaseURLMCPAliasEnv(t *testing.T) {
	env := envMap(map[string]string{"ALLOWED_BASE_URLS_EXTRA": "https://kunde.de"})
	if err := CheckBaseURL("https://kunde.de", env); err != nil {
		t.Errorf("MCP-Alias-Env sollte akzeptiert werden: %v", err)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	file, err := Load(filepath.Join(t.TempDir(), "nope", "config.json"))
	if err != nil {
		t.Fatalf("fehlende Datei darf kein Fehler sein: %v", err)
	}
	if len(file.Contexts) != 0 || file.CurrentContext != "" {
		t.Errorf("leere Config erwartet, got %+v", file)
	}
}

func TestSaveLoadRoundtripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	in := &File{
		CurrentContext: "prod",
		Contexts:       map[string]Context{"prod": {BaseURL: "https://immojump.de", Token: "geheim"}},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("0600 erwartet, got %o", perm)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.CurrentContext != "prod" || out.Contexts["prod"].Token != "geheim" {
		t.Errorf("Roundtrip verloren: %+v", out)
	}
}

func TestPathResolution(t *testing.T) {
	if got := Path(envMap(map[string]string{"IMMOJUMP_CONFIG": "/x/cfg.json"})); got != "/x/cfg.json" {
		t.Errorf("IMMOJUMP_CONFIG soll gewinnen, got %q", got)
	}
	got := Path(envMap(map[string]string{"XDG_CONFIG_HOME": "/xdg"}))
	if got != "/xdg/immojump/config.json" {
		t.Errorf("XDG-Pfad erwartet, got %q", got)
	}
	got = Path(envMap(map[string]string{"HOME": "/home/u"}))
	if got != "/home/u/.config/immojump/config.json" {
		t.Errorf("HOME-Fallback erwartet, got %q", got)
	}
}

// TestPathWithoutAnyHomeIsEmpty: Ohne HOME/XDG/IMMOJUMP_CONFIG (typisch in
// Agenten-Containern) darf das CLI keine Token-Datei ins Arbeitsverzeichnis
// schreiben — dort landet sie im Zweifel im Git-Repo des Kunden.
func TestPathWithoutAnyHomeIsEmpty(t *testing.T) {
	if got := Path(envMap(nil)); got != "" {
		t.Errorf("leerer Pfad erwartet, got %q", got)
	}
}

func TestLoadWithoutPathIsEmptyConfig(t *testing.T) {
	file, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") darf kein Fehler sein: %v", err)
	}
	if file == nil || len(file.Contexts) != 0 || file.CurrentContext != "" {
		t.Errorf("leere Config erwartet, got %+v", file)
	}
}

func TestSaveWithoutPathFails(t *testing.T) {
	err := Save("", &File{})
	if err == nil {
		t.Fatal("Save(\"\") muss scheitern statt irgendwohin zu schreiben")
	}
	if !strings.Contains(err.Error(), EnvConfig) {
		t.Errorf("Meldung soll %s nennen, got %q", EnvConfig, err.Error())
	}
}

// TestSaveIsAtomic: Ein Abbruch mitten im Schreiben darf die bestehende
// Konfiguration nicht zerstören — deshalb Temp-Datei plus Rename.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	first := &File{CurrentContext: "a", Contexts: map[string]Context{"a": {Token: "tok-a"}}}
	if err := Save(path, first); err != nil {
		t.Fatalf("Save: %v", err)
	}
	second := &File{CurrentContext: "b", Contexts: map[string]Context{"b": {Token: "tok-b"}}}
	if err := Save(path, second); err != nil {
		t.Fatalf("Save (überschreiben): %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.CurrentContext != "b" || out.Contexts["b"].Token != "tok-b" {
		t.Errorf("zweiter Stand erwartet, got %+v", out)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("0600 auch nach dem Überschreiben erwartet: %v / %v", err, info)
	}

	// Keine Temp-Reste im Zielverzeichnis.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "config.json" {
			t.Errorf("unerwartete Datei im Zielverzeichnis: %q", entry.Name())
		}
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b", []string{"a", "b"}},
		{" a , b ", []string{"a", "b"}},
		{"", nil},
		{",", nil},
		{" , , ", nil},
		{"a,,b", []string{"a", "b"}},
	}
	for _, tc := range cases {
		got := SplitCSV(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("SplitCSV(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("SplitCSV(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}
