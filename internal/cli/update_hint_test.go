package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// latestLookups zählt die Abrufe von /releases/latest.
func (f *fakeRelease) latestLookups() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.paths {
		if p == "/releases/latest" {
			n++
		}
	}
	return n
}

func warningLines(t *testing.T, stderr string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if line == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("stderr-Zeile ist kein JSON: %q", line)
		}
		if parsed["warning"] == true {
			out = append(out, parsed)
		}
	}
	return out
}

// TestUpdateHintShownWhenNewer: Ein Mensch am Terminal erfährt, dass es eine
// neuere Version gibt — als Warnzeile auf stderr, stdout bleibt unberührt.
func TestUpdateHintShownWhenNewer(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true

	code, stdout, stderr := u.run("version")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if stdout != "v0.4.0\n" {
		t.Errorf("stdout verändert: %q", stdout)
	}
	warnings := warningLines(t, stderr)
	if len(warnings) != 1 {
		t.Fatalf("genau eine Warnzeile erwartet, stderr %q", stderr)
	}
	w := warnings[0]
	msg, _ := w["message"].(string)
	if !strings.Contains(msg, "v0.10.0") || !strings.Contains(msg, "immojump update") {
		t.Errorf("Hinweis nennt Version und Befehl nicht: %q", msg)
	}
	if w["installed"] != "v0.4.0" || w["latest"] != "v0.10.0" || w["update_command"] != "immojump update" {
		t.Errorf("Details fehlen: %v", w)
	}
}

// TestUpdateHintOncePerDay: Höchstens ein Abruf und ein Hinweis pro Tag —
// sonst wird der Hinweis zu Lärm und jeder Aufruf langsamer.
func TestUpdateHintOncePerDay(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true

	u.run("version")
	u.now = u.now.Add(23 * time.Hour)
	_, _, stderr := u.run("version")
	if release.latestLookups() != 1 {
		t.Errorf("innerhalb von 24 h genau ein Abruf erwartet, got %d", release.latestLookups())
	}
	if stderr != "" {
		t.Errorf("zweiter Aufruf am selben Tag soll still sein, stderr %q", stderr)
	}

	u.now = u.now.Add(2 * time.Hour)
	_, _, stderr = u.run("version")
	if release.latestLookups() != 2 {
		t.Errorf("nach 24 h neuer Abruf erwartet, got %d", release.latestLookups())
	}
	if len(warningLines(t, stderr)) != 1 {
		t.Errorf("nach 24 h erneuter Hinweis erwartet, stderr %q", stderr)
	}
}

// TestUpdateHintSilentForMachines: Agenten, Skripte und CI bekommen keinen
// täglichen Hinweis und lösen keinen Abruf aus — für sie ist der Hinweis auf
// die Mindestversion des Servers da.
func TestUpdateHintSilentForMachines(t *testing.T) {
	cases := map[string]func(u *updateRun){
		"kein Terminal": func(u *updateRun) { u.interactive = false },
		"CI":            func(u *updateRun) { u.env["CI"] = "true" },
		"abgeschaltet":  func(u *updateRun) { u.env["IMMOJUMP_NO_UPDATE_CHECK"] = "1" },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			release := newFakeRelease(t, "v0.10.0", []byte("neu"))
			u := newUpdateRun(t, "v0.4.0", release)
			u.interactive = true
			setup(u)
			_, _, stderr := u.run("version")
			if release.latestLookups() != 0 {
				t.Errorf("kein Abruf erwartet, got %d", release.latestLookups())
			}
			if stderr != "" {
				t.Errorf("stderr soll leer sein, got %q", stderr)
			}
		})
	}
}

// TestUpdateHintSilentWhenCurrent: Aktuelle Version — kein Hinweis.
func TestUpdateHintSilentWhenCurrent(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.10.0", release)
	u.interactive = true
	if _, _, stderr := u.run("version"); stderr != "" {
		t.Errorf("stderr soll leer sein, got %q", stderr)
	}
}

// TestUpdateHintSilentForDevBuild: Ein Entwicklungs-Build fragt nicht nach.
func TestUpdateHintSilentForDevBuild(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "dev", release)
	u.interactive = true
	_, _, stderr := u.run("version")
	if release.latestLookups() != 0 || stderr != "" {
		t.Errorf("kein Abruf und kein Hinweis erwartet (Abrufe %d, stderr %q)", release.latestLookups(), stderr)
	}
}

// TestUpdateHintFailureIsSilent: Ist GitHub nicht erreichbar, merkt der
// Nutzer nichts — und es wird erst morgen wieder versucht, nicht bei jedem
// Aufruf.
func TestUpdateHintFailureIsSilent(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	release.latest = 503
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true

	code, stdout, stderr := u.run("version")
	if code != 0 || stdout != "v0.4.0\n" || stderr != "" {
		t.Errorf("Aufruf muss unberührt bleiben: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	u.now = u.now.Add(time.Hour)
	u.run("version")
	if release.latestLookups() != 1 {
		t.Errorf("nach einem Fehlschlag kein erneuter Abruf am selben Tag, got %d", release.latestLookups())
	}
}

// TestUpdateHintKeepsExitCode: Der Hinweis ändert nie den Exit-Code eines
// gescheiterten Befehls.
func TestUpdateHintKeepsExitCode(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true
	code, _, stderr := u.run("quatsch")
	if code != 2 {
		t.Errorf("Exit 2 erwartet, got %d", code)
	}
	// Die Fehlerzeile bleibt die letzte auf stderr — kein Hinweis dahinter.
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil || last["error"] != true {
		t.Errorf("letzte stderr-Zeile muss der Fehler sein, stderr %q", stderr)
	}
	if len(warningLines(t, stderr)) != 0 {
		t.Errorf("kein Update-Hinweis nach einem Fehler, stderr %q", stderr)
	}
}

// TestUpdateHintKeepsConfigNamedLikeState: Heißt die Konfiguration selbst
// update-check.json, bleibt sie unangetastet — sonst wären Contexts und
// Tokens weg.
func TestUpdateHintKeepsConfigNamedLikeState(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true
	cfg := filepath.Join(t.TempDir(), "update-check.json")
	original := []byte(`{"current_context":"prod","contexts":{"prod":{"token":"geheim"}}}`)
	if err := os.WriteFile(cfg, original, 0o600); err != nil {
		t.Fatal(err)
	}
	u.env["IMMOJUMP_CONFIG"] = cfg

	u.run("version")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(original) {
		t.Errorf("Konfiguration überschrieben: %s", raw)
	}
}

// TestUpdateHintNotDuringUpdate: `update` sieht selbst nach — ein zweiter
// Abruf und ein Hinweis auf den gerade laufenden Befehl wären doppelt.
func TestUpdateHintNotDuringUpdate(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true
	_, _, stderr := u.run("update", "--check")
	if release.latestLookups() != 1 {
		t.Errorf("genau ein Abruf erwartet, got %d", release.latestLookups())
	}
	if stderr != "" {
		t.Errorf("kein Hinweis während update, stderr %q", stderr)
	}
}

// TestUpdateHintDoesNotWaitForSlowLookup: Ein langsames Netz verzögert den
// Aufruf höchstens um die kurze Nachfrist, nicht um den vollen Timeout — und
// der Abruf wird erst morgen wiederholt.
func TestUpdateHintDoesNotWaitForSlowLookup(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	release.latestDelay = 1500 * time.Millisecond
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true

	start := time.Now()
	code, stdout, stderr := u.run("version")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Aufruf wartete %v auf den Abruf", elapsed)
	}
	if code != 0 || stdout != "v0.4.0\n" || stderr != "" {
		t.Errorf("Aufruf muss unberührt bleiben: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	u.now = u.now.Add(time.Hour)
	u.run("version")
	if release.latestLookups() > 1 {
		t.Errorf("nach einem zu langsamen Abruf kein erneuter Abruf am selben Tag, got %d", release.latestLookups())
	}
}

// TestUpdateHintSkippedWithoutState: Lässt sich der Stand nicht speichern,
// unterbleibt der Abruf — sonst kostete jeder Aufruf erneut Zeit.
func TestUpdateHintSkippedWithoutState(t *testing.T) {
	release := newFakeRelease(t, "v0.10.0", []byte("neu"))
	u := newUpdateRun(t, "v0.4.0", release)
	u.interactive = true
	blocker := filepath.Join(t.TempDir(), "keinordner")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	u.env["IMMOJUMP_CONFIG"] = filepath.Join(blocker, "config.json")

	if _, _, stderr := u.run("version"); stderr != "" {
		t.Errorf("stderr soll leer sein, got %q", stderr)
	}
	if release.latestLookups() != 0 {
		t.Errorf("ohne speicherbaren Stand kein Abruf erwartet, got %d", release.latestLookups())
	}
}
