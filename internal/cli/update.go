package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/immoJUMP/immojump-cli/internal/api"
)

// DefaultReleaseURL ist das Repo, dessen Releases `update` lädt.
const DefaultReleaseURL = "https://github.com/immoJUMP/immojump-cli"

// UpdateCommand ist der Befehl, den Hinweise auf eine neue Version nennen.
const UpdateCommand = "immojump update"

// maxBinarySize begrenzt den Download — ein Binary ist ~10 MB, alles weit
// darüber ist kein Binary.
const maxBinarySize = 200 << 20

// currentExecutable liefert den echten Pfad des laufenden Binarys — durch
// Symlinks hindurch, sonst ersetzte update den Link statt der Datei.
func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// releaseAsset ist der Dateiname des Binarys für diese Plattform — derselbe,
// den `make release-build` erzeugt.
func releaseAsset(goos, goarch string) string {
	name := "immojump-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// releaseClient baut den HTTP-Client für GitHub. Der Token des Nutzers geht
// nie mit — dieser Client kennt ihn nicht. Ohne followRedirects ist die
// Weiterleitung selbst die Antwort (/releases/latest → Tag); Downloads leitet
// GitHub auf einen Speicher-Host weiter, dem gefolgt werden muss.
func releaseClient(base *http.Client, timeout time.Duration, followRedirects bool, origin string) *http.Client {
	client := &http.Client{Timeout: timeout}
	if base != nil {
		copied := *base
		client = &copied
		if client.Timeout == 0 || client.Timeout > timeout {
			client.Timeout = timeout
		}
	}
	if followRedirects {
		client.CheckRedirect = releaseRedirectPolicy(origin)
	} else {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

// trustedReleaseHosts sind die Speicher-Hosts, auf die GitHub Release-
// Downloads weiterleitet. Nur dorthin (und zur Release-Quelle selbst) folgt
// update einer Weiterleitung — sonst könnte ein Redirect Binary und
// SHA256SUMS.txt gemeinsam aus einer fremden Quelle liefern, und die
// Prüfsumme passte trotzdem.
var trustedReleaseHosts = map[string]bool{
	"github.com":                            true,
	"release-assets.githubusercontent.com":  true,
	"objects.githubusercontent.com":         true,
	"github-releases.githubusercontent.com": true,
}

// errUntrustedRedirect: Eine Weiterleitung zeigte aus der offiziellen
// Release-Quelle heraus.
var errUntrustedRedirect = errors.New("Weiterleitung außerhalb der offiziellen Release-Quelle")

// releaseRedirectPolicy erlaubt Weiterleitungen nur innerhalb der
// Release-Quelle (origin, Schema und Host gleich) oder per HTTPS auf einen
// GitHub-Speicher-Host. HTTPS→HTTP und fremde Hosts brechen ab.
func releaseRedirectPolicy(origin string) func(*http.Request, []*http.Request) error {
	base, _ := url.Parse(origin)
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("zu viele Weiterleitungen")
		}
		target := req.URL
		if base != nil && target.Scheme == base.Scheme && strings.EqualFold(target.Host, base.Host) {
			return nil
		}
		if target.Scheme == "https" && (target.Port() == "" || target.Port() == "443") &&
			trustedReleaseHosts[strings.ToLower(target.Hostname())] {
			return nil
		}
		return fmt.Errorf("%w: %s://%s", errUntrustedRedirect, target.Scheme, target.Host)
	}
}

// releaseErr übersetzt einen HTTP-Fehler beim Release-Abruf. 5xx und
// Netzwerk sind temporär (Exit 8), alles andere nicht (Exit 1).
func releaseErr(status int, target string) *api.Error {
	err := &api.Error{
		Status:  status,
		Message: fmt.Sprintf("Release-Abruf fehlgeschlagen: HTTP %d von %s", status, target),
		Code:    "RELEASE_UNAVAILABLE",
	}
	if status < 500 {
		err.Exit = 1
	}
	return err
}

func releaseNetworkErr(target string, err error) *api.Error {
	return &api.Error{
		Message: fmt.Sprintf("Verbindung zu %s fehlgeschlagen: %v", target, err),
		Code:    "NETWORK",
		Err:     err,
	}
}

// latestRelease liest den Tag der neuesten Version aus der Weiterleitung von
// /releases/latest. Bewusst nicht über api.github.com: Deren anonymes Limit
// (60 Abrufe je Stunde und IP) teilen sich alle hinter einem Firmen-NAT.
func latestRelease(client *http.Client, base string) (string, error) {
	target := strings.TrimRight(base, "/") + "/releases/latest"
	res, err := client.Get(target)
	if err != nil {
		return "", releaseNetworkErr(target, err)
	}
	_ = res.Body.Close()
	if res.StatusCode < 300 || res.StatusCode >= 400 {
		if res.StatusCode >= 400 {
			return "", releaseErr(res.StatusCode, target)
		}
		return "", &api.Error{Message: "Keine Weiterleitung von " + target + " — neueste Version nicht ermittelbar",
			Code: "RELEASE_UNAVAILABLE", Exit: 1}
	}
	tag := path.Base(res.Header.Get("Location"))
	if _, ok := parseVersion(tag); !ok {
		return "", &api.Error{Message: fmt.Sprintf("Unerwartete Weiterleitung von %s: %q", target, res.Header.Get("Location")),
			Code: "RELEASE_UNAVAILABLE", Exit: 1}
	}
	return tag, nil
}

// fetchRelease lädt eine Datei eines Releases vollständig.
func fetchRelease(client *http.Client, base, tag, name string) ([]byte, int, error) {
	target := strings.TrimRight(base, "/") + "/releases/download/" + tag + "/" + name
	res, err := client.Get(target)
	if errors.Is(err, errUntrustedRedirect) {
		return nil, 0, &api.Error{
			Message: fmt.Sprintf("%s: %v — nichts installiert", target, err),
			Code:    "RELEASE_UNTRUSTED",
			Exit:    1,
		}
	}
	if err != nil {
		return nil, 0, releaseNetworkErr(target, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, releaseErr(res.StatusCode, target)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBinarySize+1))
	if err != nil {
		return nil, 0, releaseNetworkErr(target, err)
	}
	if len(raw) > maxBinarySize {
		return nil, 0, &api.Error{Message: target + " ist größer als erwartet — abgebrochen", Code: "RELEASE_UNAVAILABLE", Exit: 1}
	}
	return raw, res.StatusCode, nil
}

// expectedChecksum sucht den Eintrag für name in SHA256SUMS.txt
// (Format von `shasum -a 256`: "<hex>  <datei>").
func expectedChecksum(sums []byte, name string) string {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func checksumMissing(name, tag string) *api.Error {
	return &api.Error{
		Message: fmt.Sprintf("Keine Prüfsumme für %s im Release %s — ohne Prüfsumme wird nichts installiert", name, tag),
		Code:    "CHECKSUM_MISSING",
		Exit:    1,
	}
}

// downloadVerified lädt das Binary und prüft es gegen SHA256SUMS.txt.
// Fail-closed: Fehlt die Prüfsumme, gibt es kein Binary.
func downloadVerified(client *http.Client, base, tag, name string) ([]byte, error) {
	sums, status, err := fetchRelease(client, base, tag, "SHA256SUMS.txt")
	if status == http.StatusNotFound {
		return nil, checksumMissing(name, tag)
	}
	if err != nil {
		return nil, err
	}
	want := expectedChecksum(sums, name)
	if want == "" {
		return nil, checksumMissing(name, tag)
	}
	binary, _, err := fetchRelease(client, base, tag, name)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(binary)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, &api.Error{
			Message: fmt.Sprintf("Prüfsumme von %s stimmt nicht (erwartet %s, geladen %s) — nichts installiert", name, want, got),
			Code:    "CHECKSUM_MISMATCH",
			Exit:    1,
		}
	}
	return binary, nil
}

// writeNextTo legt das neue Binary als temporäre Datei im Zielordner ab —
// im selben Dateisystem, damit das Umbenennen atomar ist.
func writeNextTo(exe string, binary []byte) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".immojump-update-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, writeErr := tmp.Write(binary)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(name, 0o755)); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// replaceExecutable setzt tmp an die Stelle von exe. Unter Unix ersetzt
// rename atomar, auch während das alte Binary läuft. Windows sperrt ein
// laufendes .exe gegen Überschreiben, nicht gegen Umbenennen: Das alte wandert
// deshalb nach .old (ein Rest des letzten Updates wird vorher entfernt).
func replaceExecutable(exe, tmp, goos string) error {
	if goos != "windows" {
		return os.Rename(tmp, exe)
	}
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		// Zurück an den alten Platz — kein Zustand ohne Binary.
		return errors.Join(err, os.Rename(old, exe))
	}
	return nil
}

func writeErr(exe string, err error) *api.Error {
	message := fmt.Sprintf("%s lässt sich nicht ersetzen: %v", exe, err)
	if errors.Is(err, os.ErrPermission) {
		message = fmt.Sprintf("Keine Schreibrechte für %s. Mit Rechten erneut ausführen (sudo %s) "+
			"oder über install.sh nach ~/.local/bin installieren.", filepath.Dir(exe), UpdateCommand)
	}
	return &api.Error{Message: message, Code: "UPDATE_WRITE_FAILED", Exit: 1,
		Details: map[string]any{"path": exe}}
}

// runUpdate ist `immojump update [--check]`.
func (r *runner) runUpdate(flags *flagValues) int {
	if _, ok := parseVersion(r.version); !ok {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("Entwicklungs-Build (%s) — keine vergleichbare Version. "+
				"Ein Release installiert install.sh: curl -fsSL https://raw.githubusercontent.com/immoJUMP/immojump-cli/main/install.sh | sh",
				r.version),
			Code: "DEV_BUILD",
			Exit: 1,
		})
	}
	timeout, err := parseTimeout(flags, api.DefaultTimeout)
	if err != nil {
		return r.fail(err)
	}
	latest, err := latestRelease(releaseClient(r.http, timeout, false, r.releaseURL), r.releaseURL)
	if err != nil {
		return r.fail(err)
	}
	older, _ := versionOlder(r.version, latest)

	result := map[string]any{"installed": r.version, "latest": latest}
	if flags.bool("check") {
		result["update_available"] = older
		if older {
			result["update_command"] = UpdateCommand
		}
		return r.emit(result, flags)
	}
	if !older {
		result["updated"] = false
		return r.emit(result, flags)
	}

	exe, err := r.executable()
	if err != nil {
		return r.fail(&api.Error{Message: "Pfad des laufenden Binarys nicht ermittelbar: " + err.Error(),
			Code: "UPDATE_WRITE_FAILED", Exit: 1})
	}
	binary, err := downloadVerified(releaseClient(r.http, timeout, true, r.releaseURL), r.releaseURL, latest, releaseAsset(runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return r.fail(err)
	}
	tmp, err := writeNextTo(exe, binary)
	if err != nil {
		return r.fail(writeErr(exe, err))
	}
	if err := replaceExecutable(exe, tmp, runtime.GOOS); err != nil {
		_ = os.Remove(tmp)
		return r.fail(writeErr(exe, err))
	}
	result["updated"] = true
	result["path"] = exe
	return r.emit(result, flags)
}
