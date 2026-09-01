package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// tokenPageSegment ist die Seite, auf der ein Kunde seinen API-Token erzeugt
// (immobilien-ka/src/App.tsx: /settings/api-access).
const tokenPageSegment = "/settings/api-access"

// tokenPageURL baut die Adresse der Token-Seite der jeweiligen Instanz — nie
// die kanonische Marken-Domain: Auf Beta- und White-Label-Instanzen existiert
// der Token der Hauptinstanz nicht.
func tokenPageURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + tokenPageSegment
}

// stdinIsTerminal sagt, ob ein Mensch tippt. Im Test ist stdin ein Reader und
// nie ein Terminal — dort wird also weder ein Browser geöffnet noch ein
// Prompt geschrieben.
func (r *runner) stdinIsTerminal() bool {
	file, ok := r.stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// openBrowser öffnet eine URL im Standardbrowser. Fehler sind bewusst
// folgenlos: Der Flow funktioniert auch ohne, die URL steht ja im Prompt.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// readTokenInteractively holt den Token, wenn keiner per Flag oder Umgebung
// kam: am Terminal mit Browser und Prompt, sonst schlicht von stdin
// (`echo "$TOKEN" | immojump auth login`).
func (r *runner) readTokenInteractively(baseURL string, flags *flagValues) (string, error) {
	page := tokenPageURL(baseURL)

	if r.stdinIsTerminal() {
		// Klartext statt JSON-Zeile: Diesen Prompt sieht ausschliesslich ein
		// Mensch. Für jeden maschinellen Aufruf (Pipe, CI, Agent) ist stdin
		// kein Terminal, und stderr bleibt bei den drei JSON-Zeilenformen.
		if flags.bool("no-browser") {
			fmt.Fprintf(r.stderr, "Token erzeugen unter: %s\n", page)
		} else {
			fmt.Fprintf(r.stderr, "Browser wird geöffnet: %s\n", page)
			openBrowser(page)
		}
		fmt.Fprint(r.stderr, "Token einfügen und Enter drücken: ")
	}

	line, err := bufio.NewReader(r.stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", configErr(
			"Kein Token gelesen. Token erzeugen unter %s, dann `immojump auth login --token <token>` "+
				"oder den Token nach `immojump auth login` pipen.", page)
	}
	token := cleanToken(line)
	if token == "" {
		return "", configErr(
			"Leerer Token. Token erzeugen unter %s und vollständig einfügen.", page)
	}
	return token, nil
}

// cleanToken putzt, was beim Kopieren aus der Web-App mitkommt: Leerraum und
// ein versehentlich mitkopiertes "Bearer ".
func cleanToken(raw string) string {
	token := strings.TrimSpace(raw)
	if rest, found := strings.CutPrefix(token, "Bearer "); found {
		token = strings.TrimSpace(rest)
	}
	return token
}
