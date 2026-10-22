package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// EnvNoUpdateCheck schaltet den täglichen Hinweis auf eine neue Version ab.
const EnvNoUpdateCheck = "IMMOJUMP_NO_UPDATE_CHECK"

// updateHintInterval: höchstens ein Abruf (und ein Hinweis) pro Zeitraum.
const updateHintInterval = 24 * time.Hour

// updateHintTimeout begrenzt, wie lange ein Aufruf auf den Abruf wartet. Er
// läuft parallel zum eigentlichen Befehl; meist ist er dann längst fertig.
const updateHintTimeout = 2 * time.Second

// updateCheckState liegt neben der config.json.
type updateCheckState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
}

// updateHintWanted entscheidet, ob dieser Aufruf nachsieht. Nur ein Mensch
// am Terminal bekommt den Hinweis; Agenten, Skripte und CI erfahren über die
// Mindestversion des Servers, wenn ein Update wirklich nötig ist.
func (r *runner) updateHintWanted(args []string) bool {
	if !r.interactive || r.getenv("CI") != "" || r.getenv(EnvNoUpdateCheck) != "" {
		return false
	}
	if _, ok := parseVersion(r.version); !ok {
		return false
	}
	// update sieht selbst nach.
	if positionals, _, err := parseArgs(args); err == nil && len(positionals) > 0 && positionals[0] == "update" {
		return false
	}
	return true
}

func updateStatePath(getenv func(string) string) string {
	cfg := config.Path(getenv)
	if cfg == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cfg), "update-check.json")
}

// startUpdateHint stößt den Abruf parallel zum Befehl an und liefert eine
// Funktion, die nach dem Befehl wartet und gegebenenfalls den Hinweis
// schreibt. Jeder Fehler bleibt still: Ein Hinweis darf keinen Aufruf
// stören, und der nächste Versuch kommt ohnehin morgen.
func (r *runner) startUpdateHint(args []string) func() {
	if !r.updateHintWanted(args) {
		return func() {}
	}
	statePath := updateStatePath(r.getenv)
	if statePath == "" {
		return func() {}
	}
	now := r.now()
	if raw, err := os.ReadFile(statePath); err == nil {
		var state updateCheckState
		if json.Unmarshal(raw, &state) == nil && now.Sub(state.CheckedAt) < updateHintInterval && !state.CheckedAt.After(now) {
			return func() {}
		}
	}

	done := make(chan string, 1)
	go func() {
		latest, err := latestRelease(releaseClient(r.http, updateHintTimeout, false), r.releaseURL)
		if err != nil {
			latest = ""
		}
		done <- latest
	}()
	return func() {
		latest := <-done
		state := updateCheckState{CheckedAt: now, Latest: latest}
		if raw, err := json.Marshal(state); err == nil {
			// Ohne Schreibrecht im Config-Ordner gibt es eben jeden Tag
			// mehrere Abrufe — kein Grund, den Aufruf zu stören.
			_ = os.MkdirAll(filepath.Dir(statePath), 0o700)
			_ = os.WriteFile(statePath, raw, 0o600)
		}
		if older, ok := versionOlder(r.version, latest); ok && older {
			_ = output.WriteWarning(r.stderr, fmt.Sprintf(
				"Neue Version %s verfügbar (installiert: %s). Aktualisieren: %s",
				latest, r.version, UpdateCommand), map[string]any{
				"installed":      r.version,
				"latest":         latest,
				"update_command": UpdateCommand,
			})
		}
	}
}

// IsTerminal meldet, ob f ein Terminal ist — ohne Abhängigkeit über die
// Standardbibliothek hinaus.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0 && !strings.EqualFold(os.Getenv("TERM"), "dumb")
}
