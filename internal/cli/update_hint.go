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

// updateHintTimeout begrenzt den Abruf selbst. Er läuft parallel zum
// eigentlichen Befehl; meist ist er dann längst fertig.
const updateHintTimeout = 2 * time.Second

// updateHintGrace: So lange wartet ein Aufruf nach seinem Befehl höchstens
// noch auf den Abruf. Ist er bis dahin nicht fertig, endet der Aufruf ohne
// Hinweis — auch `immojump version` hängt offline nie die vollen 2 s.
var updateHintGrace = 300 * time.Millisecond

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
	state := filepath.Join(filepath.Dir(cfg), "update-check.json")
	// IMMOJUMP_CONFIG darf jeden Dateinamen tragen — auch update-check.json.
	// Dann gäbe es keinen eigenen Platz für den Stand; ihn zu schreiben
	// überschriebe Contexts und Tokens.
	if filepath.Clean(state) == filepath.Clean(cfg) {
		return ""
	}
	return state
}

// startUpdateHint stößt den Abruf parallel zum Befehl an und liefert eine
// Funktion, die nach dem Befehl wartet und gegebenenfalls den Hinweis
// schreibt. Jeder Fehler bleibt still: Ein Hinweis darf keinen Aufruf
// stören, und der nächste Versuch kommt ohnehin morgen.
func (r *runner) startUpdateHint(args []string) func(code int) {
	if !r.updateHintWanted(args) {
		return func(int) {}
	}
	statePath := updateStatePath(r.getenv)
	if statePath == "" {
		return func(int) {}
	}
	now := r.now()
	if raw, err := os.ReadFile(statePath); err == nil {
		var state updateCheckState
		if json.Unmarshal(raw, &state) == nil && now.Sub(state.CheckedAt) < updateHintInterval && !state.CheckedAt.After(now) {
			return func(int) {}
		}
	}

	// Den Tag vor dem Abruf belegen: Ein Abruf, der nicht rechtzeitig fertig
	// wird oder scheitert, wird erst morgen wiederholt. Lässt sich der Stand
	// nicht speichern, unterbleibt der Abruf ganz — sonst kostete jeder
	// Aufruf erneut Wartezeit.
	if !writeUpdateState(statePath, updateCheckState{CheckedAt: now}) {
		return func(int) {}
	}

	done := make(chan string, 1)
	go func() {
		latest, err := latestRelease(releaseClient(r.http, updateHintTimeout, false, r.releaseURL), r.releaseURL)
		if err != nil {
			latest = ""
		}
		done <- latest
	}()
	return func(code int) {
		var latest string
		select {
		case latest = <-done:
		case <-time.After(updateHintGrace):
			return
		}
		if latest != "" {
			_ = writeUpdateState(statePath, updateCheckState{CheckedAt: now, Latest: latest})
		}
		// Nach einem Fehler bleibt die Fehlerzeile die letzte auf stderr —
		// darauf verlassen sich Auswertungen; der Hinweis kommt morgen wieder.
		if code != 0 {
			return
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

// writeUpdateState speichert den Stand des täglichen Abrufs; false, wenn das
// nicht geht (etwa ohne Schreibrecht im Config-Ordner).
func writeUpdateState(statePath string, state updateCheckState) bool {
	raw, err := json.Marshal(state)
	if err != nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return false
	}
	return os.WriteFile(statePath, raw, 0o600) == nil
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
