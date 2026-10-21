package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// dataroom legt einen kleinen Datenraum an und liefert sein Verzeichnis.
func dataroom(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root
}

// uploadBackend antwortet wie die Route mit report=true: jede Datei wird
// zu einem Dokument mit dem secure_filename-Namen.
type uploadBackend struct {
	batches [][]string
	fail    func(n int) *reply
}

func (b *uploadBackend) handle(rec *recorded) reply {
	names := []string{}
	if rec.Multipart != nil {
		for _, header := range rec.Multipart.File["files[]"] {
			names = append(names, header.Filename)
		}
	}
	b.batches = append(b.batches, names)
	if b.fail != nil {
		if out := b.fail(len(b.batches)); out != nil {
			return *out
		}
	}
	docs := []string{}
	for i, name := range names {
		docs = append(docs, fmt.Sprintf(`{"id":%d,"dateiname":%q,"unit_id":null}`, len(b.batches)*100+i, secureFilename(name)))
	}
	return reply{body: fmt.Sprintf(`{"documents":[%s],"duplicates":[],"failed":[],"empty":[]}`, strings.Join(docs, ","))}
}

type uploadOut struct {
	Uploaded []struct {
		ID        int    `json:"id"`
		Dateiname string `json:"dateiname"`
		File      string `json:"file"`
	} `json:"uploaded"`
	Duplicates []map[string]any `json:"duplicates"`
	Failed     []struct {
		File    string `json:"file"`
		Message string `json:"message"`
	} `json:"failed"`
	Empty []string `json:"empty"`
}

func decodeUpload(t *testing.T, stdout string) uploadOut {
	t.Helper()
	var out uploadOut
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("Bericht als JSON erwartet: %v (%q)", err, stdout)
	}
	return out
}

func TestUploadExpandsFoldersAndSkipsHiddenFiles(t *testing.T) {
	root := dataroom(t, map[string]string{
		"a.pdf": "A", "b.pdf": "B", ".hidden.pdf": "H", ".DS_Store": "x", "Thumbs.db": "x",
		"sub/c.pdf": "C", ".git/d.pdf": "D",
	})
	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = b.handle
	code, stdout, stderr := h.run("documents", "upload", root, "--immobilie-id", "5")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if fmt.Sprint(b.batches) != "[[a.pdf b.pdf]]" {
		t.Errorf("ohne --recursive nur die Dateien direkt im Ordner erwartet, got %v", b.batches)
	}

	h2 := newHarness(t)
	b2 := &uploadBackend{}
	h2.handle = b2.handle
	code, stdout, stderr = h2.run("documents", "upload", root, "--recursive", "--immobilie-id", "5", "--batch-files", "2")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if fmt.Sprint(b2.batches) != "[[a.pdf b.pdf] [c.pdf]]" {
		t.Errorf("rekursiv in Stapeln zu 2 erwartet, got %v", b2.batches)
	}
	out := decodeUpload(t, stdout)
	if len(out.Uploaded) != 3 || out.Uploaded[2].File != filepath.Join(root, "sub", "c.pdf") {
		t.Errorf("drei Uploads mit lokalem Pfad erwartet, got %+v", out.Uploaded)
	}
	if out.Failed == nil || out.Duplicates == nil || out.Empty == nil {
		t.Error("leere Listen sollen als [] erscheinen, nicht als null")
	}
}

func TestUploadExplicitHiddenFileIsUploaded(t *testing.T) {
	root := dataroom(t, map[string]string{".notiz.pdf": "N"})
	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = b.handle
	if code, _, stderr := h.run("documents", "upload", filepath.Join(root, ".notiz.pdf")); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.batches) != 1 || len(b.batches[0]) != 1 {
		t.Errorf("ausdrücklich genannte Datei soll hochgeladen werden, got %v", b.batches)
	}
}

func TestUploadBatchesBySize(t *testing.T) {
	root := dataroom(t, map[string]string{
		"1.pdf": strings.Repeat("x", 700), "2.pdf": strings.Repeat("x", 700), "3.pdf": strings.Repeat("x", 100),
	})
	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = b.handle
	// 0.001 MB = 1048 Bytes: 1.pdf allein, 2.pdf + 3.pdf zusammen.
	if code, _, stderr := h.run("documents", "upload", root, "--batch-mb", "0.001"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if fmt.Sprint(b.batches) != "[[1.pdf] [2.pdf 3.pdf]]" {
		t.Errorf("Stapel nach Größe erwartet, got %v", b.batches)
	}
}

func TestUploadSendsFormFieldsContentTypeAndLength(t *testing.T) {
	root := dataroom(t, map[string]string{"Mietvertrag.pdf": "%PDF-1.4", "daten.ijzzq": "?"})
	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = b.handle
	code, _, stderr := h.run("documents", "upload",
		filepath.Join(root, "Mietvertrag.pdf"), filepath.Join(root, "daten.ijzzq"),
		"--immobilie-id", "5", "--unit-id", "u-1", "--category", "mietvertrag", "--allow-duplicate")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	form := h.last.Multipart
	for key, want := range map[string]string{
		"organisation_id": "org-test", "immobilien_id": "5", "unit_id": "u-1",
		"category": "mietvertrag", "allow_duplicate_upload": "true", "report": "true",
	} {
		if got := form.Value[key]; len(got) != 1 || got[0] != want {
			t.Errorf("Formfeld %s=%s erwartet, got %v", key, want, got)
		}
	}
	types := map[string]string{}
	for _, header := range form.File["files[]"] {
		types[header.Filename] = header.Header.Get("Content-Type")
	}
	if types["Mietvertrag.pdf"] != "application/pdf" || types["daten.ijzzq"] != "application/octet-stream" {
		t.Errorf("Content-Type je Datei erwartet, got %v", types)
	}
	if h.last.ContentLength <= 0 {
		t.Errorf("Content-Length vorab berechnet erwartet (kein chunked), got %d", h.last.ContentLength)
	}
}

func TestUploadUnitNeedsImmobilie(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A"})
	h := newHarness(t)
	code, _, stderr := h.run("documents", "upload", root, "--unit-id", "u-1")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("kein Request erwartet")
	}
}

func TestUploadMissingPathFailsBeforeUploading(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A"})
	h := newHarness(t)
	code, _, stderr := h.run("documents", "upload", filepath.Join(root, "a.pdf"), filepath.Join(root, "fehlt.pdf"))
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("bei fehlender Datei darf nichts hochgeladen werden")
	}
	h2 := newHarness(t)
	empty := dataroom(t, map[string]string{".DS_Store": "x"})
	if code, _, _ := h2.run("documents", "upload", empty); code != 2 {
		t.Errorf("Ordner ohne Dateien: Exit 2 erwartet, got %d", code)
	}
}

func TestUploadMapsReportToLocalFiles(t *testing.T) {
	root := dataroom(t, map[string]string{
		"Mietvertrag WE 01 Müller.pdf": "A", "dup.pdf": "B", "kaputt.pdf": "C", "leer.pdf": "",
	})
	h := newHarness(t)
	h.respond = `{"documents":[{"id":7,"dateiname":"Mietvertrag_WE_01_Muller.pdf","unit_id":null}],` +
		`"duplicates":[{"fileName":"dup.pdf","existingDocumentId":3}],` +
		`"failed":[{"fileName":"kaputt.pdf","message":"Invalid PDF"}],"empty":["leer.pdf"]}`
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 bei fehlgeschlagener Datei erwartet, got %d (%s)", code, stderr)
	}
	out := decodeUpload(t, stdout)
	if len(out.Uploaded) != 1 || out.Uploaded[0].File != filepath.Join(root, "Mietvertrag WE 01 Müller.pdf") || out.Uploaded[0].ID != 7 {
		t.Errorf("Dokument dem lokalen Pfad zugeordnet erwartet, got %+v", out.Uploaded)
	}
	if len(out.Duplicates) != 1 || out.Duplicates[0]["file"] != filepath.Join(root, "dup.pdf") || out.Duplicates[0]["existingDocumentId"] != float64(3) {
		t.Errorf("Duplikat mit Pfad und Backend-Feldern erwartet, got %v", out.Duplicates)
	}
	if len(out.Failed) != 1 || out.Failed[0].File != filepath.Join(root, "kaputt.pdf") || out.Failed[0].Message != "Invalid PDF" {
		t.Errorf("Fehlschlag mit Pfad und Meldung erwartet, got %+v", out.Failed)
	}
	if len(out.Empty) != 1 || out.Empty[0] != filepath.Join(root, "leer.pdf") {
		t.Errorf("leere Datei mit Pfad erwartet, got %v", out.Empty)
	}
	if line := errorLine(t, stderr); line["code"] != "UPLOAD_INCOMPLETE" {
		t.Errorf("Fehlerzeile UPLOAD_INCOMPLETE erwartet, got %v", line)
	}
}

func TestUploadDuplicatesAloneAreSuccess(t *testing.T) {
	root := dataroom(t, map[string]string{"dup.pdf": "B"})
	h := newHarness(t)
	h.respond = `{"documents":[],"duplicates":[{"fileName":"dup.pdf"}],"failed":[],"empty":[]}`
	code, _, stderr := h.run("documents", "upload", root)
	if code != 0 || stderr != "" {
		t.Errorf("Duplikate sind kein Fehler: Exit 0 und leeres stderr erwartet, got %d %q", code, stderr)
	}
}

// TestUploadUnderstandsLegacyResponse: Eine Instanz ohne report=true liefert
// das Array und die Listen in X-Upload-*-Headern.
func TestUploadUnderstandsLegacyResponse(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "dup.pdf": "B"})
	h := newHarness(t)
	h.handle = func(rec *recorded) reply {
		return reply{
			body:   `[{"id":1,"dateiname":"a.pdf","extrahierter_text":"lang"}]`,
			header: http.Header{"X-Upload-Duplicate-Files": {`[{"fileName":"dup.pdf","existingDocumentId":2}]`}},
		}
	}
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	out := decodeUpload(t, stdout)
	if len(out.Uploaded) != 1 || out.Uploaded[0].File != filepath.Join(root, "a.pdf") || len(out.Duplicates) != 1 {
		t.Errorf("Array plus Header ausgewertet erwartet, got %+v", out)
	}
	if strings.Contains(stdout, "extrahierter_text") {
		t.Error("der Bericht soll schlank bleiben")
	}
}

func TestUploadContinuesAfterServerErrorBatch(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B", "c.pdf": "C"})
	h := newHarness(t)
	b := &uploadBackend{fail: func(n int) *reply {
		if n == 1 {
			return &reply{status: 502, body: `{"message":"Bad Gateway"}`}
		}
		return nil
	}}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "upload", root, "--batch-files", "2")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if len(b.batches) != 2 {
		t.Errorf("Uploads werden nie wiederholt; zwei Stapel erwartet, got %d", len(b.batches))
	}
	out := decodeUpload(t, stdout)
	if len(out.Failed) != 2 || len(out.Uploaded) != 1 || out.Failed[0].Message != "Bad Gateway" {
		t.Errorf("Stapel 1 fehlgeschlagen, Stapel 2 hochgeladen erwartet, got %+v", out)
	}
}

func TestUploadStopsOnClientErrorAfterProgress(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B", "c.pdf": "C"})
	h := newHarness(t)
	b := &uploadBackend{fail: func(n int) *reply {
		if n == 2 {
			return &reply{status: 403, body: `{"message":"Keine Berechtigung."}`}
		}
		return nil
	}}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "upload", root, "--batch-files", "1")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if len(b.batches) != 2 {
		t.Errorf("nach 403 keine weiteren Stapel erwartet, got %d", len(b.batches))
	}
	out := decodeUpload(t, stdout)
	if len(out.Uploaded) != 1 || len(out.Failed) != 2 {
		t.Errorf("a hochgeladen, b und c fehlgeschlagen erwartet, got %+v", out)
	}
}

// TestUploadSingleBatchKeepsExitCodes: Ein Aufruf mit einem Stapel verhält
// sich wie früher — der Fehler des Backends mit seinem Exit-Code.
func TestUploadSingleBatchKeepsExitCodes(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A"})
	for status, exit := range map[int]int{400: 11, 403: 6, 503: 8} {
		h := newHarness(t)
		h.status = status
		h.respond = `{"message":"nein"}`
		code, stdout, _ := h.run("documents", "upload", filepath.Join(root, "a.pdf"))
		if code != exit {
			t.Errorf("HTTP %d: Exit %d erwartet, got %d", status, exit, code)
		}
		if stdout != "" {
			t.Errorf("HTTP %d: stdout soll leer bleiben, got %q", status, stdout)
		}
		if len(h.requests) != 1 {
			t.Errorf("HTTP %d: Uploads nie wiederholen, got %d Versuche", status, len(h.requests))
		}
	}
}

func TestUploadTimeoutDefault(t *testing.T) {
	flags := newFlagValues()
	if got, _ := parseTimeout(flags, uploadTimeout); got != 300*time.Second {
		t.Errorf("Upload-Default 300 s erwartet, got %v", got)
	}
	flags.add("timeout", "20")
	if got, _ := parseTimeout(flags, uploadTimeout); got != 20*time.Second {
		t.Errorf("--timeout soll gewinnen, got %v", got)
	}
}

func TestSecureFilenameMatchesWerkzeug(t *testing.T) {
	cases := map[string]string{
		"Mietvertrag WE 01 Müller.pdf": "Mietvertrag_WE_01_Muller.pdf",
		"../../etc/passwd":             "etc_passwd",
		"Straße 5.pdf":                 "Strae_5.pdf",
		"  Exposé (final).pdf":         "Expose_final.pdf",
	}
	keys := make([]string, 0, len(cases))
	for k := range cases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, in := range keys {
		if got := secureFilename(in); got != cases[in] {
			t.Errorf("secureFilename(%q) = %q, erwartet %q", in, got, cases[in])
		}
	}
}

// TestUploadAbortOnFirstBatchNamesWhatWasNotSent: Bricht der erste von
// mehreren Stapeln ab (gemessen: 400, weil der Objektspeicher der Instanz
// fehlte), muss die Fehlerzeile sagen, dass der Rest nie gesendet wurde —
// sonst hält der Agent 10 von 32 Dateien für den ganzen Datenraum.
func TestUploadAbortOnFirstBatchNamesWhatWasNotSent(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B", "c.pdf": "C"})
	h := newHarness(t)
	h.status = 400
	h.respond = `{"error":"Failed to process all files: a.pdf: Processing error"}`
	code, stdout, stderr := h.run("documents", "upload", root, "--batch-files", "1")
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != "" || len(h.requests) != 1 {
		t.Errorf("leeres stdout und ein Request erwartet, got %q / %d", stdout, len(h.requests))
	}
	line := errorLine(t, stderr)
	if line["batch"] != float64(1) || line["batches"] != float64(3) || line["files_total"] != float64(3) || line["files_not_sent"] != float64(2) {
		t.Errorf("Stapel- und Dateizahlen in der Fehlerzeile erwartet, got %v", line)
	}
	if !strings.HasPrefix(line["message"].(string), "Failed to process all files") {
		t.Errorf("Backend-Meldung unverändert erwartet, got %q", line["message"])
	}
}

func TestUploadRejectsSymlinks(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "geheim.txt": "G"})
	link := filepath.Join(root, "link.pdf")
	if err := os.Symlink(filepath.Join(root, "geheim.txt"), link); err != nil {
		t.Skipf("Symlinks nicht verfügbar: %v", err)
	}
	h := newHarness(t)
	code, _, stderr := h.run("documents", "upload", link)
	if code != 2 || h.last != nil {
		t.Errorf("ausdrücklich genannter Symlink: Exit 2 ohne Request erwartet, got %d (%s)", code, stderr)
	}
	h2 := newHarness(t)
	b := &uploadBackend{}
	h2.handle = b.handle
	if code, _, stderr := h2.run("documents", "upload", root); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if fmt.Sprint(b.batches) != "[[a.pdf geheim.txt]]" {
		t.Errorf("Symlink im Ordner soll übersprungen werden, got %v", b.batches)
	}
}

// TestUploadDropsUnreadableFileAndSendsTheRest: Scheitert eine Datei an der
// Prüfung vor dem Senden, geht kein Request raus — sie landet unter failed,
// der Rest des Stapels wird gesendet.
func TestUploadDropsUnreadableFileAndSendsTheRest(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root sind Dateirechte wirkungslos")
	}
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	if err := os.Chmod(filepath.Join(root, "a.pdf"), 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer func() { _ = os.Chmod(filepath.Join(root, "a.pdf"), 0o600) }()
	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if fmt.Sprint(b.batches) != "[[b.pdf]]" {
		t.Errorf("nur b.pdf gesendet erwartet, got %v", b.batches)
	}
	out := decodeUpload(t, stdout)
	if len(out.Failed) != 1 || out.Failed[0].File != filepath.Join(root, "a.pdf") || len(out.Uploaded) != 1 {
		t.Errorf("a.pdf unter failed, b.pdf hochgeladen erwartet, got %+v", out)
	}
}
