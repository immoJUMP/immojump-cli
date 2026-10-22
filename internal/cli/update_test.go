package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRelease spielt GitHub: /releases/latest leitet auf den Tag um, unter
// /releases/download/<tag>/ liegen Binary und SHA256SUMS.txt.
type fakeRelease struct {
	t       *testing.T
	server  *httptest.Server
	tag     string
	binary  []byte
	sums    string // leer: korrekte Prüfsumme wird erzeugt
	noSums  bool
	latest  int // Status für /releases/latest (0 = 302)
	mu      sync.Mutex
	paths   []string
	headers []http.Header
}

func assetName() string {
	name := "immojump-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func newFakeRelease(t *testing.T, tag string, binary []byte) *fakeRelease {
	t.Helper()
	f := &fakeRelease{t: t, tag: tag, binary: binary}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		download := "/releases/download/" + f.tag + "/"
		switch {
		case r.URL.Path == "/releases/latest":
			if f.latest != 0 {
				w.WriteHeader(f.latest)
				return
			}
			http.Redirect(w, r, f.server.URL+"/releases/tag/"+f.tag, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, download):
			// Wie GitHub: Downloads leiten auf den Speicher-Host weiter.
			http.Redirect(w, r, f.server.URL+"/assets/"+strings.TrimPrefix(r.URL.Path, download), http.StatusFound)
		case r.URL.Path == "/assets/"+assetName():
			_, _ = w.Write(f.binary)
		case r.URL.Path == "/assets/SHA256SUMS.txt" && !f.noSums:
			sums := f.sums
			if sums == "" {
				sum := sha256.Sum256(f.binary)
				sums = hex.EncodeToString(sum[:]) + "  " + assetName() + "\n" +
					strings.Repeat("0", 64) + "  immojump-plan9-mips\n"
			}
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeRelease) downloaded() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.paths {
		if p == "/assets/"+assetName() {
			return true
		}
	}
	return false
}

// updateRun ruft Run mit eigener Release-Quelle und eigenem Executable auf.
type updateRun struct {
	t           *testing.T
	version     string
	release     *fakeRelease
	exe         string
	env         map[string]string
	interactive bool
	now         time.Time
}

func newUpdateRun(t *testing.T, version string, release *fakeRelease) *updateRun {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "immojump")
	if err := os.WriteFile(exe, []byte("alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &updateRun{
		t: t, version: version, release: release, exe: exe,
		env: map[string]string{
			"IMMOJUMP_CONFIG": filepath.Join(t.TempDir(), "config.json"),
		},
		now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}
}

func (u *updateRun) run(args ...string) (int, string, string) {
	u.t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run(args, Options{
		Stdin:       strings.NewReader(""),
		Stdout:      stdout,
		Stderr:      stderr,
		Getenv:      func(key string) string { return u.env[key] },
		Version:     u.version,
		ReleaseURL:  u.release.server.URL,
		Executable:  func() (string, error) { return u.exe, nil },
		Interactive: u.interactive,
		Now:         func() time.Time { return u.now },
	})
	return code, stdout.String(), stderr.String()
}

func (u *updateRun) exeContent() string {
	u.t.Helper()
	raw, err := os.ReadFile(u.exe)
	if err != nil {
		u.t.Fatal(err)
	}
	return string(raw)
}

func decodeObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("stdout ist kein JSON-Objekt: %q (%v)", raw, err)
	}
	return out
}

// TestUpdateReplacesBinary: Der Normalfall — neuere Version vorhanden,
// Prüfsumme stimmt, das laufende Binary wird ersetzt und bleibt ausführbar.
func TestUpdateReplacesBinary(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)

	code, stdout, stderr := u.run("update")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (stderr %q)", code, stderr)
	}
	if got := u.exeContent(); got != "neu" {
		t.Errorf("Binary nicht ersetzt, Inhalt %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(u.exe)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("Binary nicht ausführbar: %v", info.Mode())
		}
	}
	out := decodeObject(t, stdout)
	if out["installed"] != "v0.4.0" || out["latest"] != "v0.10.0" || out["updated"] != true {
		t.Errorf("unerwartete Ausgabe: %v", out)
	}
	if out["path"] != u.exe {
		t.Errorf("path %q erwartet, got %v", u.exe, out["path"])
	}
}

// TestUpdateLeavesNoTempFiles: Nach einem Update liegt neben dem Binary nichts
// Halbfertiges herum.
func TestUpdateLeavesNoTempFiles(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	if code, _, stderr := u.run("update"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	entries, err := os.ReadDir(filepath.Dir(u.exe))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("nur das Binary erwartet, gefunden: %v", names)
	}
}

// TestUpdateAlreadyCurrent: Ist die installierte Version aktuell, wird nichts
// geladen und nichts angefasst.
func TestUpdateAlreadyCurrent(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.10.0", release)

	code, stdout, _ := u.run("update")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if release.downloaded() {
		t.Error("Binary geladen, obwohl schon aktuell")
	}
	if got := u.exeContent(); got != "alt" {
		t.Errorf("Binary verändert: %q", got)
	}
	out := decodeObject(t, stdout)
	if out["updated"] != false || out["latest"] != "v0.10.0" {
		t.Errorf("unerwartete Ausgabe: %v", out)
	}
}

// TestUpdateCheckOnlyReports: --check sagt nur, ob es etwas Neues gibt.
func TestUpdateCheckOnlyReports(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)

	code, stdout, _ := u.run("update", "--check")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if release.downloaded() {
		t.Error("--check darf nichts herunterladen")
	}
	if got := u.exeContent(); got != "alt" {
		t.Errorf("Binary verändert: %q", got)
	}
	out := decodeObject(t, stdout)
	if out["update_available"] != true || out["installed"] != "v0.4.0" || out["latest"] != "v0.10.0" {
		t.Errorf("unerwartete Ausgabe: %v", out)
	}
}

// TestUpdateRejectsChecksumMismatch: Stimmt die Prüfsumme nicht, bleibt das
// alte Binary — ein manipulierter oder abgebrochener Download landet nie auf
// dem PATH.
func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	release.sums = strings.Repeat("a", 64) + "  " + assetName() + "\n"
	u := newUpdateRun(t, "v0.4.0", release)

	code, _, stderr := u.run("update")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if got := errorLine(t, stderr)["code"]; got != "CHECKSUM_MISMATCH" {
		t.Errorf("code CHECKSUM_MISMATCH erwartet, got %v", got)
	}
	if got := u.exeContent(); got != "alt" {
		t.Errorf("Binary trotz falscher Prüfsumme ersetzt: %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(u.exe))
	if len(entries) != 1 {
		t.Errorf("Reste des Downloads liegen herum: %d Einträge", len(entries))
	}
}

// TestUpdateRequiresChecksum: Fehlt die Prüfsummendatei oder der Eintrag für
// dieses Binary, wird nicht installiert — fail-closed.
func TestUpdateRequiresChecksum(t *testing.T) {
	t.Run("Datei fehlt", func(t *testing.T) {
		release := newFakeRelease(t, "v0.10.0", []byte("neu"))
		release.noSums = true
		u := newUpdateRun(t, "v0.4.0", release)
		code, _, stderr := u.run("update")
		if code == 0 {
			t.Fatal("Fehler erwartet")
		}
		if got := errorLine(t, stderr)["code"]; got != "CHECKSUM_MISSING" {
			t.Errorf("code CHECKSUM_MISSING erwartet, got %v", got)
		}
		if got := u.exeContent(); got != "alt" {
			t.Errorf("Binary ohne Prüfsumme ersetzt: %q", got)
		}
	})
	t.Run("Eintrag fehlt", func(t *testing.T) {
		release := newFakeRelease(t, "v0.10.0", []byte("neu"))
		release.sums = strings.Repeat("a", 64) + "  immojump-plan9-mips\n"
		u := newUpdateRun(t, "v0.4.0", release)
		code, _, stderr := u.run("update")
		if code != 1 {
			t.Fatalf("Exit 1 erwartet, got %d", code)
		}
		if got := errorLine(t, stderr)["code"]; got != "CHECKSUM_MISSING" {
			t.Errorf("code CHECKSUM_MISSING erwartet, got %v", got)
		}
		if got := u.exeContent(); got != "alt" {
			t.Errorf("Binary ohne Prüfsumme ersetzt: %q", got)
		}
	})
}

// TestUpdateRefusesDevBuild: Ein Entwicklungs-Build hat keine vergleichbare
// Version — überschreiben wäre geraten.
func TestUpdateRefusesDevBuild(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "dev", release)
	code, _, stderr := u.run("update")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if got := errorLine(t, stderr)["code"]; got != "DEV_BUILD" {
		t.Errorf("code DEV_BUILD erwartet, got %v", got)
	}
	if got := u.exeContent(); got != "alt" {
		t.Errorf("Entwicklungs-Build überschrieben: %q", got)
	}
}

// TestUpdateBlockedByReadonly: update verändert die Maschine. Ein Agent unter
// --readonly darf das nicht.
func TestUpdateBlockedByReadonly(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	code, _, _ := u.run("update", "--readonly")
	if code != 6 {
		t.Fatalf("Exit 6 erwartet, got %d", code)
	}
	if got := u.exeContent(); got != "alt" {
		t.Errorf("Binary trotz --readonly ersetzt: %q", got)
	}
}

// TestUpdateCheckAllowedReadonly: Nachsehen ändert nichts und bleibt auch
// unter --readonly erlaubt.
func TestUpdateCheckAllowedReadonly(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	if code, _, stderr := u.run("update", "--check", "--readonly"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
}

// TestUpdateServerErrorIsRetryable: 5xx bei GitHub ist temporär — Exit 8.
func TestUpdateServerErrorIsRetryable(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	release.latest = http.StatusServiceUnavailable
	u := newUpdateRun(t, "v0.4.0", release)
	code, _, _ := u.run("update")
	if code != 8 {
		t.Fatalf("Exit 8 erwartet, got %d", code)
	}
}

// TestUpdateSendsNoToken: Der immoJUMP-Token hat bei GitHub nichts verloren.
func TestUpdateSendsNoToken(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.env["IMMOJUMP_TOKEN"] = "geheim"
	if code, _, stderr := u.run("update"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	for _, h := range release.headers {
		if h.Get("Authorization") != "" {
			t.Errorf("Authorization an GitHub geschickt: %q", h.Get("Authorization"))
		}
	}
}

// TestReplaceExecutableWindows: Unter Windows lässt sich ein laufendes .exe
// nicht überschreiben, wohl aber umbenennen. Das alte landet als .old daneben.
func TestReplaceExecutableWindows(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "immojump.exe")
	if err := os.WriteFile(exe, []byte("alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Rest eines früheren Updates darf nicht stören.
	if err := os.WriteFile(exe+".old", []byte("uralt"), 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "neu.tmp")
	if err := os.WriteFile(tmp, []byte("neu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(exe, tmp, "windows"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(exe); string(raw) != "neu" {
		t.Errorf("neues Binary erwartet, got %q", raw)
	}
	if raw, _ := os.ReadFile(exe + ".old"); string(raw) != "alt" {
		t.Errorf("altes Binary als .old erwartet, got %q", raw)
	}
}
