// Package config kapselt die lokale CLI-Konfiguration: Kontexte nach dem
// kubectl-Vorbild, die Auflösungskette Flag > Env > Context-Datei sowie die
// Allowlist erlaubter immoJUMP-Instanzen.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultBaseURL ist die Instanz, die ohne jede Angabe verwendet wird.
const DefaultBaseURL = "https://immojump.de"

// Namen der unterstützten Umgebungsvariablen (identisch zum MCP-Server).
const (
	EnvToken     = "IMMOJUMP_TOKEN"
	EnvOrg       = "IMMOJUMP_ORGANISATION_ID"
	EnvBaseURL   = "IMMOJUMP_BASE_URL"
	EnvContext   = "IMMOJUMP_CONTEXT"
	EnvConfig    = "IMMOJUMP_CONFIG"
	EnvExtraURLs = "IMMOJUMP_EXTRA_BASE_URLS"
	// EnvExtraURLsMCP ist der MCP-kompatible Alias von EnvExtraURLs.
	EnvExtraURLsMCP = "ALLOWED_BASE_URLS_EXTRA"
)

// Context beschreibt eine Instanz plus Organisation plus Token.
type Context struct {
	BaseURL        string `json:"base_url,omitempty"`
	OrganisationID string `json:"organisation_id,omitempty"`
	Token          string `json:"token,omitempty"`
	TokenEnv       string `json:"token_env,omitempty"`
}

// File ist der Inhalt von ~/.config/immojump/config.json.
type File struct {
	CurrentContext string             `json:"current_context,omitempty"`
	Contexts       map[string]Context `json:"contexts"`
}

// Overrides sind die pro Aufruf gesetzten globalen Flags.
type Overrides struct {
	Context string
	BaseURL string
	Org     string
}

// Resolved ist das Ergebnis der Auflösungskette.
type Resolved struct {
	BaseURL     string
	Org         string
	Token       string
	ContextName string
}

// getenv macht eine fehlende env-Funktion unschädlich.
func getenv(env func(string) string) func(string) string {
	if env == nil {
		return func(string) string { return "" }
	}
	return env
}

// FirstNonEmpty liefert den ersten nicht leeren Wert — die Auflösungskette
// Flag > Env > Datei in einer Zeile.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// SplitCSV zerlegt eine kommagetrennte Liste und wirft leere Teile weg.
// Einheitliches Verhalten für --allow, --tag-ids, --fields und
// IMMOJUMP_EXTRA_BASE_URLS.
func SplitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// NormalizeBaseURL entfernt Leerzeichen und abschließende Schrägstriche.
func NormalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// Resolve bestimmt Instanz, Organisation und Token nach der Kette
// Flag > Env > Context-Datei. Ein ausdrücklich gewählter, aber unbekannter
// Context ist ein Fehler.
func Resolve(file *File, ov Overrides, env func(string) string) (Resolved, error) {
	e := getenv(env)
	if file == nil {
		file = &File{}
	}

	name := FirstNonEmpty(ov.Context, e(EnvContext), file.CurrentContext)
	var ctx Context
	if name != "" {
		found, ok := file.Contexts[name]
		if !ok {
			return Resolved{}, fmt.Errorf(
				"Context %q ist nicht konfiguriert. Vorhandene ansehen: immojump context list; anlegen: immojump auth login --context %s …",
				name, name)
		}
		ctx = found
	}

	res := Resolved{
		ContextName: name,
		BaseURL:     NormalizeBaseURL(FirstNonEmpty(ov.BaseURL, e(EnvBaseURL), ctx.BaseURL, DefaultBaseURL)),
		Org:         FirstNonEmpty(ov.Org, e(EnvOrg), ctx.OrganisationID),
		Token:       FirstNonEmpty(e(EnvToken), ContextToken(ctx, e)),
	}
	return res, nil
}

// ContextToken löst die token_env-Indirektion auf und fällt auf token zurück.
func ContextToken(ctx Context, env func(string) string) string {
	e := getenv(env)
	if ctx.TokenEnv != "" {
		if v := e(ctx.TokenEnv); v != "" {
			return v
		}
	}
	return ctx.Token
}

// AllowedBaseURLs liefert die effektive Allowlist inklusive der per Env
// ergänzten Instanzen.
func AllowedBaseURLs(env func(string) string) []string {
	e := getenv(env)
	allowed := []string{
		"https://immojump.de",
		"https://beta.immojump.de",
		"http://localhost:8081",
	}
	for _, key := range []string{EnvExtraURLs, EnvExtraURLsMCP} {
		for _, extra := range SplitCSV(e(key)) {
			if v := NormalizeBaseURL(extra); v != "" {
				allowed = append(allowed, v)
			}
		}
	}
	return allowed
}

// CheckBaseURL stellt sicher, dass Tokens nur an bekannte immoJUMP-Instanzen
// gehen — Schutz gegen Tippfehler und Look-alike-Hosts.
func CheckBaseURL(raw string, env func(string) string) error {
	candidate := NormalizeBaseURL(raw)
	if candidate == "" {
		return fmt.Errorf("Base-URL fehlt")
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Base-URL %q ist keine gültige http(s)-Adresse", raw)
	}

	allowed := AllowedBaseURLs(env)
	for _, a := range allowed {
		if strings.EqualFold(a, candidate) {
			return nil
		}
	}
	return fmt.Errorf(
		"Base-URL %q ist nicht erlaubt. Zugelassen sind: %s. Weitere Instanzen per %s=… ergänzen.",
		candidate, strings.Join(allowed, ", "), EnvExtraURLs)
}

// Path bestimmt den Ort der Konfigurationsdatei:
// IMMOJUMP_CONFIG > XDG_CONFIG_HOME > HOME.
//
// Fehlt alles drei — in Agenten-Containern durchaus üblich —, liefert Path
// bewusst einen leeren Pfad statt eines relativen. Eine Token-Datei im
// Arbeitsverzeichnis landet sonst im Zweifel im Repository des Kunden.
func Path(env func(string) string) string {
	e := getenv(env)
	if p := strings.TrimSpace(e(EnvConfig)); p != "" {
		return p
	}
	if xdg := strings.TrimSpace(e("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "immojump", "config.json")
	}
	if home := strings.TrimSpace(e("HOME")); home != "" {
		return filepath.Join(home, ".config", "immojump", "config.json")
	}
	return ""
}

// Load liest die Konfiguration. Eine fehlende Datei ist kein Fehler, sondern
// eine leere Konfiguration; ein leerer Pfad ebenso (siehe Path).
func Load(path string) (*File, error) {
	if strings.TrimSpace(path) == "" {
		return &File{Contexts: map[string]Context{}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Contexts: map[string]Context{}}, nil
		}
		return nil, fmt.Errorf("Konfiguration %s nicht lesbar: %w", path, err)
	}
	file := &File{}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, file); err != nil {
			return nil, fmt.Errorf("Konfiguration %s ist kein gültiges JSON: %w", path, err)
		}
	}
	if file.Contexts == nil {
		file.Contexts = map[string]Context{}
	}
	return file, nil
}

// Save schreibt die Konfiguration mit engen Rechten (Verzeichnis 0700,
// Datei 0600) — sie enthält Tokens.
//
// Geschrieben wird atomar: erst in eine Temp-Datei im Zielverzeichnis, dann
// ein Rename. Ein Abbruch mittendrin lässt so die alte Konfiguration heil,
// statt eine halbe Datei zu hinterlassen, die keinen Context mehr enthält.
func Save(path string, file *File) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf(
			"Kein Config-Pfad ermittelbar — %s setzen (oder HOME bzw. XDG_CONFIG_HOME)", EnvConfig)
	}
	if file == nil {
		file = &File{}
	}
	if file.Contexts == nil {
		file.Contexts = map[string]Context{}
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("Verzeichnis %s nicht anlegbar: %w", dir, err)
		}
	}
	if dir == "" {
		dir = "."
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("Konfiguration nicht serialisierbar: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".immojump-config-*.tmp")
	if err != nil {
		return fmt.Errorf("Konfiguration %s nicht schreibbar: %w", path, err)
	}
	tmpName := tmp.Name()
	// Ab hier darf nichts liegen bleiben, egal wo es scheitert.
	cleanup := func(cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return cause
	}
	if err := tmp.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("Rechte auf %s nicht setzbar: %w", tmpName, err))
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(fmt.Errorf("Konfiguration %s nicht schreibbar: %w", path, err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("Konfiguration %s nicht schreibbar: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("Konfiguration %s nicht schreibbar: %w", path, err)
	}
	return nil
}

// MaskToken kürzt ein Token für die Anzeige — nie den Klartext ausgeben.
func MaskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", 6) + token[len(token)-4:]
}
