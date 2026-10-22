package cli

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/output"
)

// MinVersionHeader trägt die älteste CLI-Version, mit der das Backend noch
// zusammenarbeitet. Gesetzt vom immoJUMP-Backend (config `immojump_cli`).
const MinVersionHeader = "X-Immojump-Cli-Min-Version"

// checkMinVersion schreibt einen Hinweis, wenn der Server eine neuere
// Mindestversion meldet. Anders als der tägliche Hinweis gilt er auch ohne
// Terminal: Er ist der Weg, auf dem ein Agent erfährt, dass seine Aufrufe
// gegen einen veränderten API-Vertrag laufen. Er steht vor einer etwaigen
// Fehlerzeile, die damit die letzte Zeile bleibt.
func (r *runner) checkMinVersion(header http.Header) {
	if r.minVersionWarned {
		return
	}
	minimum := strings.TrimSpace(header.Get(MinVersionHeader))
	older, ok := versionOlder(r.version, minimum)
	if !ok || !older {
		return
	}
	r.minVersionWarned = true
	_ = output.WriteWarning(r.stderr, fmt.Sprintf(
		"Diese CLI-Version (%s) ist älter als die Mindestversion %s des Servers — einzelne Befehle können scheitern oder falsch wirken. Aktualisieren: %s",
		r.version, minimum, UpdateCommand), map[string]any{
		"installed":      r.version,
		"minimum":        minimum,
		"update_command": UpdateCommand,
	})
}
