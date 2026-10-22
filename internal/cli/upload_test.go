package cli

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
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
		unit := "null"
		if rec.Multipart != nil && len(rec.Multipart.Value["unit_id"]) == 1 {
			unit = fmt.Sprintf("%q", rec.Multipart.Value["unit_id"][0])
		}
		docs = append(docs, fmt.Sprintf(`{"id":%d,"dateiname":%q,"unit_id":%s,"sourceFile":%q}`, len(b.batches)*100+i, secureFilename(name), unit, name))
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
	h.respond = `{"documents":[],"duplicates":[{"fileName":"dup.pdf","sourceFile":"dup.pdf"}],"failed":[],"empty":[]}`
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

// TestUploadStopsOnClientErrorAfterProgress: Ein 401/403/429 ab Stapel 2
// beendet den Lauf — mit dem Exit-Code genau dieses Fehlers (Agenten
// branchen darauf), der Bericht bleibt auf stdout.
func TestUploadStopsOnClientErrorAfterProgress(t *testing.T) {
	for status, exit := range map[int]int{401: 4, 403: 6, 429: 7} {
		root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B", "c.pdf": "C"})
		h := newHarness(t)
		b := &uploadBackend{fail: func(n int) *reply {
			if n == 2 {
				return &reply{status: status, body: `{"message":"Halt."}`}
			}
			return nil
		}}
		h.handle = b.handle
		code, stdout, stderr := h.run("documents", "upload", root, "--batch-files", "1", "--retries", "0")
		if code != exit {
			t.Fatalf("HTTP %d: Exit %d erwartet, got %d (%s)", status, exit, code, stderr)
		}
		if len(b.batches) != 2 {
			t.Errorf("HTTP %d: nach dem Fehler keine weiteren Stapel erwartet, got %d", status, len(b.batches))
		}
		out := decodeUpload(t, stdout)
		if len(out.Uploaded) != 1 || len(out.Failed) != 2 {
			t.Errorf("HTTP %d: a hochgeladen, b und c fehlgeschlagen erwartet, got %+v", status, out)
		}
		if line := errorLine(t, stderr); line["status"] != float64(status) || line["message"] != "Halt." {
			t.Errorf("HTTP %d: Fehlerzeile des Backends erwartet, got %v", status, line)
		}
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
	if len(h.requests) != 1 {
		t.Errorf("ein Request erwartet, got %d", len(h.requests))
	}
	// Der Bericht steht trotzdem auf stdout: alle drei Dateien unter failed.
	if out := decodeUpload(t, stdout); len(out.Failed) != 3 || len(out.Uploaded) != 0 {
		t.Errorf("Bericht mit drei fehlgeschlagenen Dateien erwartet, got %+v", out)
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

// TestUploadSplitsCollidingNamesIntoSeparateBatches: Das Backend meldet
// Dateien nur über ihren (secure_filename-)Namen. Zwei gleichnamige Dateien
// in einem Stapel ließen sich im Bericht nicht unterscheiden — A/vertrag.pdf
// (Duplikat) und B/vertrag.pdf (neu) würden vertauscht.
func TestUploadSplitsCollidingNamesIntoSeparateBatches(t *testing.T) {
	root := dataroom(t, map[string]string{"A/vertrag.pdf": "alt", "B/vertrag.pdf": "neu", "B/x.pdf": "x"})
	h := newHarness(t)
	calls := 0
	h.handle = func(rec *recorded) reply {
		calls++
		docs, dups := []string{}, []string{}
		for _, header := range rec.Multipart.File["files[]"] {
			if header.Filename == "vertrag.pdf" && calls == 1 {
				// Der erste Stapel trägt A/vertrag.pdf — das gibt es schon.
				dups = append(dups, `{"fileName":"vertrag.pdf","existingDocumentId":3}`)
				continue
			}
			docs = append(docs, fmt.Sprintf(`{"id":%d,"dateiname":%q}`, calls*10+len(docs), header.Filename))
		}
		return reply{body: fmt.Sprintf(`{"documents":[%s],"duplicates":[%s],"failed":[],"empty":[]}`,
			strings.Join(docs, ","), strings.Join(dups, ","))}
	}
	code, stdout, stderr := h.run("documents", "upload", root, "--recursive")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 2 {
		t.Fatalf("zwei Stapel erwartet (Namen kollidieren), got %d", len(h.requests))
	}
	out := decodeUpload(t, stdout)
	if out.Duplicates[0]["file"] != filepath.Join(root, "A", "vertrag.pdf") {
		t.Errorf("Duplikat gehört zu A/vertrag.pdf, got %v", out.Duplicates[0]["file"])
	}
	files := map[string]bool{}
	for _, up := range out.Uploaded {
		files[up.File] = true
	}
	if len(out.Uploaded) != 2 || !files[filepath.Join(root, "B", "vertrag.pdf")] || !files[filepath.Join(root, "B", "x.pdf")] {
		t.Errorf("Uploads gehören zu B/, got %+v", out.Uploaded)
	}
}

func TestBatchFilesSeparatesNormalizedNameCollisions(t *testing.T) {
	files := []localFile{
		{path: "a/Vertrag.PDF", size: 1}, {path: "b/vertrag.pdf", size: 1},
		{path: "c/Müller.pdf", size: 1}, {path: "d/Muller.pdf", size: 1},
		{path: "e/plan.docx", size: 1}, {path: "f/plan.pdf", size: 1},
		{path: "g/einzeln.pdf", size: 1},
	}
	for _, batch := range batchFiles(files, 10, 1<<30) {
		seen := map[string]string{}
		for _, file := range batch {
			key := uploadNameKey(file.path)
			if other, ok := seen[key]; ok {
				t.Errorf("%s und %s im selben Stapel", other, file.path)
			}
			seen[key] = file.path
		}
	}
	if n := len(batchFiles(files, 10, 1<<30)); n != 2 {
		t.Errorf("zwei Stapel erwartet (je eine Kollision je Paar), got %d", n)
	}
}

// TestUploadSingleFileKeepsLegacyOutput: Bestehende Agenten (Skripte mit
// --fields id oder .[0].id) laden einzelne Dateien — für sie bleibt die
// Ausgabe das Array des Backends, unverändert.
func TestUploadSingleFileKeepsLegacyOutput(t *testing.T) {
	root := dataroom(t, map[string]string{"expose.pdf": "%PDF"})
	h := newHarness(t)
	h.respond = `[{"id":42,"dateiname":"expose.pdf","unit_id":"u-1"}]`
	code, stdout, stderr := h.run("documents", "upload", filepath.Join(root, "expose.pdf"),
		"--immobilie-id", "5", "--unit-id", "u-1", "--category", "expose", "--fields", "id")
	if code != 0 || stderr != "" {
		t.Fatalf("Exit 0 und leeres stderr erwartet, got %d (%s)", code, stderr)
	}
	if strings.TrimSpace(stdout) != `[{"id":42}]` {
		t.Errorf("Array des Backends erwartet, got %q", stdout)
	}
	form := h.last.Multipart
	if _, ok := form.Value["report"]; ok {
		t.Error("ohne --report kein report=true")
	}
	if form.Value["unit_id"][0] != "u-1" || form.Value["category"][0] != "expose" {
		t.Errorf("--unit-id und --category auch für die einzelne Datei erwartet, got %v", form.Value)
	}
	if h.last.ContentLength <= 0 {
		t.Error("auch die einzelne Datei wird mit bekannter Länge gestreamt")
	}
}

func TestUploadReportForReportFlagFoldersAndSeveralFiles(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	for name, args := range map[string][]string{
		"--report":        {filepath.Join(root, "a.pdf"), "--report"},
		"Ordner":          {root},
		"mehrere Dateien": {filepath.Join(root, "a.pdf"), filepath.Join(root, "b.pdf")},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			b := &uploadBackend{}
			h.handle = b.handle
			code, stdout, stderr := h.run(append([]string{"documents", "upload"}, args...)...)
			if code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
			}
			if got := h.last.Multipart.Value["report"]; len(got) != 1 || got[0] != "true" {
				t.Errorf("report=true erwartet, got %v", got)
			}
			if out := decodeUpload(t, stdout); len(out.Uploaded) == 0 {
				t.Errorf("Bericht erwartet, got %q", stdout)
			}
		})
	}
}

// TestUploadRefusesParentSwappedBetweenBatches: Während Stapel 1 läuft, wird
// datenraum/sub durch einen Symlink auf einen fremden Ordner ersetzt. Stapel 2
// darf dessen Dateien nicht senden — os.Root lässt den Datenraum nicht los.
func TestUploadRefusesParentSwappedBetweenBatches(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "datenraum")
	outside := filepath.Join(base, "fremd")
	for path, content := range map[string]string{
		filepath.Join(root, "a.pdf"): "A", filepath.Join(root, "sub", "b.pdf"): "B",
		filepath.Join(outside, "b.pdf"): "GEHEIM",
	} {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		_ = os.WriteFile(path, []byte(content), 0o600)
	}
	h := newHarness(t)
	b := &uploadBackend{}
	sent := []string{}
	h.handle = func(rec *recorded) reply {
		for _, header := range rec.Multipart.File["files[]"] {
			f, _ := header.Open()
			raw := make([]byte, 16)
			n, _ := f.Read(raw)
			sent = append(sent, string(raw[:n]))
		}
		if len(b.batches) == 0 {
			_ = os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "sub.alt"))
			if err := os.Symlink(outside, filepath.Join(root, "sub")); err != nil {
				t.Skipf("Symlinks nicht verfügbar: %v", err)
			}
		}
		return b.handle(rec)
	}
	code, stdout, _ := h.run("documents", "upload", root, "--recursive", "--batch-files", "1")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	for _, content := range sent {
		if content == "GEHEIM" {
			t.Fatal("Inhalt von außerhalb des Datenraums wurde gesendet")
		}
	}
	out := decodeUpload(t, stdout)
	if len(out.Failed) != 1 || out.Failed[0].File != filepath.Join(root, "sub", "b.pdf") || !strings.Contains(out.Failed[0].Message, "außerhalb") {
		t.Errorf("sub/b.pdf mit klarer Meldung unter failed erwartet, got %+v", out.Failed)
	}
}

// sourceReport antwortet je gesendeter Datei mit sourceFile (Backend ab
// 3af6851c7): dup → duplicates, sonst documents mit secure_filename-Namen.
func sourceReport(dup func(name string) bool, extra func(name string) []string) func(rec *recorded) reply {
	return func(rec *recorded) reply {
		docs, dups := []string{}, []string{}
		for i, header := range rec.Multipart.File["files[]"] {
			name := header.Filename
			if dup(name) {
				dups = append(dups, fmt.Sprintf(`{"fileName":%q,"existingDocumentId":3,"sourceFile":%q}`, secureFilename(name), name))
				continue
			}
			members := []string{secureFilename(name)}
			if extra != nil {
				if more := extra(name); more != nil {
					members = more
				}
			}
			for j, member := range members {
				docs = append(docs, fmt.Sprintf(`{"id":%d,"dateiname":%q,"sourceFile":%q}`, 100*i+j+1, member, name))
			}
		}
		return reply{body: fmt.Sprintf(`{"documents":[%s],"duplicates":[%s],"failed":[],"empty":[]}`,
			strings.Join(docs, ","), strings.Join(dups, ","))}
	}
}

// TestUploadMapsViaSourceFileDespiteNormalisation: Werkzeug macht aus
// „Şahin.pdf“ „Sahin.pdf“ — der gespeicherte Name verrät nicht, welche der
// beiden lokalen Dateien gemeint ist, sourceFile schon.
func TestUploadMapsViaSourceFileDespiteNormalisation(t *testing.T) {
	root := dataroom(t, map[string]string{"Şahin.pdf": "neu", "Sahin.pdf": "alt"})
	h := newHarness(t)
	h.handle = sourceReport(func(name string) bool { return name == "Sahin.pdf" }, nil)
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 0 || stderr != "" {
		t.Fatalf("Exit 0 und leeres stderr erwartet, got %d (%s)", code, stderr)
	}
	out := decodeUpload(t, stdout)
	if len(out.Uploaded) != 1 || out.Uploaded[0].File != filepath.Join(root, "Şahin.pdf") {
		t.Errorf("neues Dokument gehört zu Şahin.pdf, got %+v", out.Uploaded)
	}
	if len(out.Duplicates) != 1 || out.Duplicates[0]["file"] != filepath.Join(root, "Sahin.pdf") {
		t.Errorf("Duplikat gehört zu Sahin.pdf, got %v", out.Duplicates)
	}
}

// TestUploadMapsArchiveMembersToTheArchive: Ein ZIP-Mitglied vertrag.pdf
// heißt wie die lose vertrag.pdf daneben — zugeordnet wird über sourceFile,
// alle Mitglieder gehören zum Archiv.
func TestUploadMapsArchiveMembersToTheArchive(t *testing.T) {
	root := dataroom(t, map[string]string{"datenraum.zip": "PK", "vertrag.pdf": "lose"})
	h := newHarness(t)
	h.handle = sourceReport(func(name string) bool { return name == "vertrag.pdf" }, func(name string) []string {
		if name == "datenraum.zip" {
			return []string{"vertrag.pdf", "grundbuch.pdf"}
		}
		return nil
	})
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 2 {
		t.Errorf("zwei Stapel erwartet (ein Archiv geht allein), got %d", len(h.requests))
	}
	out := decodeUpload(t, stdout)
	zip := filepath.Join(root, "datenraum.zip")
	if len(out.Uploaded) != 2 || out.Uploaded[0].File != zip || out.Uploaded[1].File != zip {
		t.Errorf("beide Mitglieder gehören zum Archiv, got %+v", out.Uploaded)
	}
	if len(out.Duplicates) != 1 || out.Duplicates[0]["file"] != filepath.Join(root, "vertrag.pdf") {
		t.Errorf("Duplikat gehört zur losen vertrag.pdf, got %v", out.Duplicates)
	}
}

// TestUploadWarnsOnceWithoutSourceFile: Eine ältere Instanz liefert keinen
// sourceFile — dann gilt die Namens-Heuristik, mit genau einem Hinweis.
func TestUploadWarnsOnceWithoutSourceFile(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B", "c.pdf": "C"})
	h := newHarness(t)
	h.handle = func(rec *recorded) reply {
		docs := []string{}
		for i, header := range rec.Multipart.File["files[]"] {
			docs = append(docs, fmt.Sprintf(`{"id":%d,"dateiname":%q}`, i+1, header.Filename))
		}
		return reply{body: `{"documents":[` + strings.Join(docs, ",") + `],"duplicates":[],"failed":[],"empty":[]}`}
	}
	code, stdout, stderr := h.run("documents", "upload", root, "--batch-files", "1")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	lines := stderrLines(t, stderr)
	if len(lines) != 1 || lines[0]["warning"] != true || !strings.Contains(lines[0]["message"].(string), "sourceFile") {
		t.Errorf("genau eine Warnung zu sourceFile erwartet, got %q", stderr)
	}
	if out := decodeUpload(t, stdout); len(out.Uploaded) != 3 || out.Uploaded[2].File != filepath.Join(root, "c.pdf") {
		t.Errorf("Heuristik soll weiter zuordnen, got %+v", out.Uploaded)
	}
}

// TestUploadSingleArchiveReportsPartialFailure: Ein einzelnes ZIP geht über
// den bisherigen Ein-Datei-Weg. Fehlgeschlagene oder leere Mitglieder stehen
// dann nur in X-Upload-*-Headern — stdout bleibt das Array, aber Exit 0 wäre
// gelogen.
func TestUploadSingleArchiveReportsPartialFailure(t *testing.T) {
	root := dataroom(t, map[string]string{"datenraum.zip": "PK"})
	for name, header := range map[string]http.Header{
		"failed": {"X-Upload-Failed-Files": {`[{"fileName":"kaputt.pdf","message":"Invalid PDF"}]`}},
		"empty":  {"X-Upload-Empty-Files": {`["leer.pdf"]`}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.handle = func(rec *recorded) reply {
				return reply{body: `[{"id":1,"dateiname":"vertrag.pdf"}]`, header: header}
			}
			code, stdout, stderr := h.run("documents", "upload", filepath.Join(root, "datenraum.zip"))
			if code != 1 {
				t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
			}
			if strings.TrimSpace(stdout) != `[{"id":1,"dateiname":"vertrag.pdf"}]` {
				t.Errorf("stdout bleibt das Array des Backends, got %q", stdout)
			}
			line := errorLine(t, stderr)
			if line["code"] != "UPLOAD_INCOMPLETE" {
				t.Errorf("UPLOAD_INCOMPLETE erwartet, got %v", line)
			}
			if _, ok := line[name]; !ok {
				t.Errorf("Fehlerzeile soll %s als JSON tragen, got %v", name, line)
			}
		})
	}
}

// TestUploadEmptyFilesAreIncomplete: Eine leere Datei ist nicht hochgeladen —
// Exit 0 sagte dem Agenten das Gegenteil.
func TestUploadEmptyFilesAreIncomplete(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "leer.pdf": ""})
	h := newHarness(t)
	h.respond = `{"documents":[{"id":1,"dateiname":"a.pdf","sourceFile":"a.pdf"}],"duplicates":[],"failed":[],"empty":["leer.pdf"]}`
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
	}
	if out := decodeUpload(t, stdout); len(out.Empty) != 1 {
		t.Errorf("leere Datei im Bericht erwartet, got %+v", out)
	}
	line := errorLine(t, stderr)
	if line["code"] != "UPLOAD_INCOMPLETE" || !strings.Contains(line["message"].(string), "leer") {
		t.Errorf("UPLOAD_INCOMPLETE mit Hinweis auf leere Dateien erwartet, got %v", line)
	}
}

// TestUploadRejectsTooLargeFilesLocally: Eine Datei über der Grenze geht gar
// nicht erst raus — sie stünde minutenlang in der Leitung und käme als 413
// oder Timeout zurück.
func TestUploadRejectsTooLargeFilesLocally(t *testing.T) {
	root := dataroom(t, map[string]string{"klein.pdf": "A"})
	big := filepath.Join(root, "scan.pdf")
	f, err := os.Create(big)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := f.Truncate(251 << 20); err != nil { // sparse: kostet keinen Platz
		t.Fatalf("Truncate: %v", err)
	}
	_ = f.Close()

	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if fmt.Sprint(b.batches) != "[[klein.pdf]]" {
		t.Errorf("nur klein.pdf gesendet erwartet, got %v", b.batches)
	}
	out := decodeUpload(t, stdout)
	if len(out.Failed) != 1 || out.Failed[0].File != big || !strings.Contains(out.Failed[0].Message, "zu groß") {
		t.Errorf("scan.pdf als zu groß unter failed erwartet, got %+v", out.Failed)
	}

	h2 := newHarness(t)
	if code, _, stderr := h2.run("documents", "upload", filepath.Join(root, "klein.pdf"), "--max-file-mb", "0.0000001"); code != 1 || h2.last != nil {
		t.Errorf("einzelne Datei über --max-file-mb: Exit 1 ohne Request erwartet, got %d (%s)", code, stderr)
	} else if line := errorLine(t, stderr); line["code"] != "FILE_TOO_LARGE" {
		t.Errorf("FILE_TOO_LARGE erwartet, got %v", line)
	}
	h3 := newHarness(t)
	if code, _, _ := h3.run("documents", "upload", root, "--max-file-mb", "0"); code != 2 {
		t.Errorf("--max-file-mb 0: Exit 2 erwartet, got %d", code)
	}
}

// TestUploadEmptyArchiveMemberIsNotTheLooseFile: Das ZIP enthält eine leere
// leer.pdf, daneben liegt eine volle leer.pdf. empty nennt nur Namen — die
// lose Datei ist über sourceFile schon als hochgeladen erkannt und darf nicht
// zusätzlich als leer erscheinen; der leere Eintrag gehört zum Archiv.
func TestUploadEmptyArchiveMemberIsNotTheLooseFile(t *testing.T) {
	root := dataroom(t, map[string]string{"datenraum.zip": "PK", "leer.pdf": "voll"})
	h := newHarness(t)
	h.handle = func(rec *recorded) reply {
		if rec.Multipart.File["files[]"][0].Filename == "datenraum.zip" {
			return reply{body: `{"documents":[],"duplicates":[],"failed":[],"empty":["leer.pdf"]}`}
		}
		return reply{body: `{"documents":[{"id":1,"dateiname":"leer.pdf","sourceFile":"leer.pdf"}],"duplicates":[],"failed":[],"empty":[]}`}
	}
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 (leeres Mitglied) erwartet, got %d (%s)", code, stderr)
	}
	out := decodeUpload(t, stdout)
	loose := filepath.Join(root, "leer.pdf")
	if len(out.Uploaded) != 1 || out.Uploaded[0].File != loose {
		t.Errorf("lose leer.pdf hochgeladen erwartet, got %+v", out.Uploaded)
	}
	if len(out.Empty) != 1 || out.Empty[0] != filepath.Join(root, "datenraum.zip") {
		t.Errorf("leeres Mitglied gehört zum Archiv, got %v", out.Empty)
	}
}

// TestUploadMarksPossiblyStoredFiles: Nach 5xx oder Netzwerkfehler kann der
// Server die Dateien trotzdem gespeichert haben. Der Bericht sagt das
// (may_exist), statt sie als sicher fehlend zu melden; nach 413 oder einer
// lokalen Ablehnung ist nichts gespeichert.
func TestUploadMarksPossiblyStoredFiles(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	h := newHarness(t)
	b := &uploadBackend{fail: func(n int) *reply {
		switch n {
		case 1:
			return &reply{status: 504, body: `{"message":"Gateway Timeout"}`}
		case 2:
			return &reply{status: 413, body: `{"message":"Request Entity Too Large"}`}
		}
		return nil
	}}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "upload", root, "--batch-files", "1")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	var out struct {
		Failed []map[string]any `json:"failed"`
	}
	_ = json.Unmarshal([]byte(stdout), &out)
	if len(out.Failed) != 2 || out.Failed[0]["may_exist"] != true || out.Failed[1]["may_exist"] != nil {
		t.Errorf("a.pdf (504) mit may_exist, b.pdf (413) ohne erwartet, got %v", out.Failed)
	}
}

// TestUploadFirstBatchAbortKeepsLocalRejections: Bricht schon der erste
// Stapel ab, gehen die lokal abgelehnten Dateien (zu groß) nicht verloren.
func TestUploadFirstBatchAbortKeepsLocalRejections(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B", "gross.pdf": strings.Repeat("x", 4096)})
	h := newHarness(t)
	h.status = 403
	h.respond = `{"message":"Keine Berechtigung."}`
	code, stdout, stderr := h.run("documents", "upload", root, "--batch-files", "1", "--max-file-mb", "0.001")
	if code != 6 {
		t.Fatalf("Exit 6 erwartet, got %d (%s)", code, stderr)
	}
	out := decodeUpload(t, stdout)
	messages := map[string]string{}
	for _, f := range out.Failed {
		messages[filepath.Base(f.File)] = f.Message
	}
	if len(out.Failed) != 3 || !strings.Contains(messages["gross.pdf"], "zu groß") || messages["b.pdf"] == "" {
		t.Errorf("gross.pdf (zu groß), a.pdf und b.pdf unter failed erwartet, got %+v", out.Failed)
	}
}

// TestUploadDetectsIgnoredUnitID: Eine Instanz ohne unit_id-Unterstützung
// nimmt die Datei an und verwirft die Zuordnung still — der Mietvertrag hinge
// dann nur am Haus. Das CLI prüft die Antwort und meldet das.
func TestUploadDetectsIgnoredUnitID(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	t.Run("Bericht", func(t *testing.T) {
		h := newHarness(t)
		h.respond = `{"documents":[{"id":1,"dateiname":"a.pdf","sourceFile":"a.pdf"},{"id":2,"dateiname":"b.pdf","unit_id":"u-1","sourceFile":"b.pdf"}],"duplicates":[],"failed":[],"empty":[]}`
		code, stdout, stderr := h.run("documents", "upload", root, "--immobilie-id", "5", "--unit-id", "u-1")
		if code != 1 {
			t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
		}
		if out := decodeUpload(t, stdout); len(out.Uploaded) != 2 {
			t.Errorf("Bericht bleibt auf stdout, got %+v", out)
		}
		if strings.Count(stderr, "unit_id") < 1 || errorLine(t, stderr)["code"] != "UNIT_ID_IGNORED" {
			t.Errorf("UNIT_ID_IGNORED erwartet, got %q", stderr)
		}
	})
	t.Run("einzelne Datei", func(t *testing.T) {
		h := newHarness(t)
		h.respond = `[{"id":1,"dateiname":"a.pdf"}]`
		code, stdout, stderr := h.run("documents", "upload", filepath.Join(root, "a.pdf"), "--immobilie-id", "5", "--unit-id", "u-1")
		if code != 1 || strings.TrimSpace(stdout) != `[{"id":1,"dateiname":"a.pdf"}]` {
			t.Fatalf("Exit 1 und unverändertes Array erwartet, got %d %q (%s)", code, stdout, stderr)
		}
		if errorLine(t, stderr)["code"] != "UNIT_ID_IGNORED" {
			t.Errorf("UNIT_ID_IGNORED erwartet, got %q", stderr)
		}
	})
	t.Run("zugeordnet", func(t *testing.T) {
		h := newHarness(t)
		b := &uploadBackend{}
		h.handle = b.handle
		if code, _, stderr := h.run("documents", "upload", root, "--immobilie-id", "5", "--unit-id", "u-1"); code != 0 || stderr != "" {
			t.Errorf("Exit 0 und leeres stderr erwartet, got %d (%s)", code, stderr)
		}
	})
}

// TestUploadLegacyUnreadableHeaderIsNotSilent: Ein X-Upload-Failed-Files, das
// sich nicht lesen lässt, ist trotzdem ein Fehlschlag — nicht „alles gut“.
func TestUploadLegacyUnreadableHeaderIsNotSilent(t *testing.T) {
	root := dataroom(t, map[string]string{"a.zip": "PK"})
	h := newHarness(t)
	h.handle = func(rec *recorded) reply {
		return reply{body: `[]`, header: http.Header{"X-Upload-Failed-Files": {`kaputt[`}}}
	}
	code, _, stderr := h.run("documents", "upload", filepath.Join(root, "a.zip"))
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
	}
	if line := errorLine(t, stderr); line["code"] != "UPLOAD_INCOMPLETE" {
		t.Errorf("UPLOAD_INCOMPLETE erwartet, got %v", line)
	}
}

// TestUploadMaxSizeAppliesAtSendTime: b.pdf wächst, während Stapel 1 läuft,
// über die Grenze — beim Senden von Stapel 2 fällt das auf.
func TestUploadMaxSizeAppliesAtSendTime(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	h := newHarness(t)
	b := &uploadBackend{}
	h.handle = func(rec *recorded) reply {
		if len(b.batches) == 0 {
			_ = os.WriteFile(filepath.Join(root, "b.pdf"), make([]byte, 4096), 0o600)
		}
		return b.handle(rec)
	}
	code, stdout, _ := h.run("documents", "upload", root, "--batch-files", "1", "--max-file-mb", "0.001")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if fmt.Sprint(b.batches) != "[[a.pdf]]" {
		t.Errorf("nur a.pdf gesendet erwartet, got %v", b.batches)
	}
	out := decodeUpload(t, stdout)
	if len(out.Failed) != 1 || !strings.Contains(out.Failed[0].Message, "zu groß") {
		t.Errorf("b.pdf als zu groß unter failed erwartet, got %+v", out.Failed)
	}
}

// TestUploadArchivesGetTheirOwnBatch: empty nennt nur Mitgliedsnamen. Lägen
// zwei Archive in einem Stapel, wäre die Herkunft eines leeren Mitglieds
// unbekannt — deshalb geht jedes Archiv allein.
func TestUploadArchivesGetTheirOwnBatch(t *testing.T) {
	root := dataroom(t, map[string]string{"a.zip": "PK", "b.zip": "PK", "c.pdf": "C", "d.rar": "R"})
	h := newHarness(t)
	var batches [][]string
	h.handle = func(rec *recorded) reply {
		names := []string{}
		for _, header := range rec.Multipart.File["files[]"] {
			names = append(names, header.Filename)
		}
		batches = append(batches, names)
		empty := `[]`
		if strings.HasSuffix(names[0], ".zip") {
			empty = `["leer.pdf"]`
		}
		return reply{body: `{"documents":[],"duplicates":[],"failed":[],"empty":` + empty + `}`}
	}
	code, stdout, _ := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 (leere Mitglieder) erwartet, got %d", code)
	}
	for _, batch := range batches {
		archives := 0
		for _, name := range batch {
			if strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".rar") {
				archives++
			}
		}
		if archives > 0 && len(batch) != 1 {
			t.Errorf("Archiv nicht allein im Stapel: %v", batch)
		}
	}
	out := decodeUpload(t, stdout)
	if fmt.Sprint(out.Empty) != fmt.Sprint([]string{filepath.Join(root, "a.zip"), filepath.Join(root, "b.zip")}) {
		t.Errorf("leere Mitglieder je ihrem Archiv erwartet, got %v", out.Empty)
	}
}

func writeZip(t *testing.T, path string, members map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	w := zip.NewWriter(f)
	for name, content := range members {
		part, _ := w.Create(name)
		_, _ = part.Write([]byte(content))
	}
	_ = w.Close()
	_ = f.Close()
}

// TestUploadSingleEmptyFileIsIncompleteLocally: Eine leere Einzeldatei bekam
// vom Backend 400 („All files are empty“) und damit Exit 11, als wäre der
// Aufruf falsch. Sie ist schlicht nicht hochladbar: Exit 1, ohne Request.
func TestUploadSingleEmptyFileIsIncompleteLocally(t *testing.T) {
	root := dataroom(t, map[string]string{"leer.pdf": ""})
	writeZip(t, filepath.Join(root, "leer.zip"), map[string]string{"a.pdf": "", "b.pdf": ""})
	writeZip(t, filepath.Join(root, "voll.zip"), map[string]string{"a.pdf": "", "b.pdf": "%PDF"})
	for _, name := range []string{"leer.pdf", "leer.zip"} {
		h := newHarness(t)
		code, stdout, stderr := h.run("documents", "upload", filepath.Join(root, name))
		if code != 1 || h.last != nil || stdout != "" {
			t.Errorf("%s: Exit 1 ohne Request und ohne stdout erwartet, got %d (%s)", name, code, stderr)
			continue
		}
		line := errorLine(t, stderr)
		if line["code"] != "UPLOAD_INCOMPLETE" || fmt.Sprint(line["empty"]) != "["+filepath.Join(root, name)+"]" {
			t.Errorf("%s: UPLOAD_INCOMPLETE mit empty erwartet, got %v", name, line)
		}
	}
	h := newHarness(t)
	h.respond = `[{"id":1}]`
	if code, _, stderr := h.run("documents", "upload", filepath.Join(root, "voll.zip")); code != 0 || h.last == nil {
		t.Errorf("ZIP mit Inhalt soll gesendet werden, got %d (%s)", code, stderr)
	}
}

// TestUploadUnitIDNotAppliedToDuplicates: Ist die Datei schon da, legt das
// Backend kein Dokument an — und ordnet folglich auch keiner Einheit zu.
// Genau der README-Ablauf (erst der Ordner, dann der Mietvertrag mit
// --unit-id) endete so mit Exit 0, ohne Zuordnung.
func TestUploadUnitIDNotAppliedToDuplicates(t *testing.T) {
	root := dataroom(t, map[string]string{"WE-001.pdf": "A", "neu.pdf": "B"})
	t.Run("Bericht", func(t *testing.T) {
		h := newHarness(t)
		h.respond = `{"documents":[{"id":2,"dateiname":"neu.pdf","unit_id":"u-1","sourceFile":"neu.pdf"}],` +
			`"duplicates":[{"fileName":"WE-001.pdf","existingDocumentId":"d-9","existingPropertyId":"5","sourceFile":"WE-001.pdf"}],"failed":[],"empty":[]}`
		code, stdout, stderr := h.run("documents", "upload", root, "--immobilie-id", "5", "--unit-id", "u-1")
		if code != 1 {
			t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
		}
		if out := decodeUpload(t, stdout); len(out.Duplicates) != 1 {
			t.Errorf("Bericht bleibt auf stdout, got %+v", out)
		}
		line := errorLine(t, stderr)
		if line["code"] != "UNIT_ID_NOT_APPLIED" {
			t.Fatalf("UNIT_ID_NOT_APPLIED erwartet, got %v", line)
		}
		dups, _ := line["duplicates"].([]any)
		entry, _ := dups[0].(map[string]any)
		if len(dups) != 1 || entry["existingDocumentId"] != "d-9" || entry["existingPropertyId"] != "5" ||
			!strings.Contains(fmt.Sprint(entry["hint"]), "documents set-unit d-9 --unit-id u-1") {
			t.Errorf("Duplikat mit existingDocumentId, existingPropertyId und Hinweis erwartet, got %v", dups)
		}
	})
	t.Run("einzelne Datei", func(t *testing.T) {
		h := newHarness(t)
		h.handle = func(rec *recorded) reply {
			return reply{body: `[]`, header: http.Header{"X-Upload-Duplicate-Files": {
				`[{"fileName":"WE-001.pdf","existingDocumentId":"d-9","existingPropertyId":"5"}]`}}}
		}
		code, stdout, stderr := h.run("documents", "upload", filepath.Join(root, "WE-001.pdf"), "--immobilie-id", "5", "--unit-id", "u-1")
		if code != 1 || strings.TrimSpace(stdout) != "[]" {
			t.Fatalf("Exit 1 und unverändertes Array erwartet, got %d %q (%s)", code, stdout, stderr)
		}
		if line := errorLine(t, stderr); line["code"] != "UNIT_ID_NOT_APPLIED" {
			t.Errorf("UNIT_ID_NOT_APPLIED erwartet, got %v", line)
		}
	})
	t.Run("ohne --unit-id bleibt ein Duplikat Erfolg", func(t *testing.T) {
		h := newHarness(t)
		h.respond = `{"documents":[],"duplicates":[{"fileName":"WE-001.pdf","existingDocumentId":"d-9","sourceFile":"WE-001.pdf"}],"failed":[],"empty":[]}`
		if code, _, stderr := h.run("documents", "upload", filepath.Join(root, "WE-001.pdf"), "--report"); code != 0 {
			t.Errorf("Exit 0 erwartet, got %d (%s)", code, stderr)
		}
	})
}

// TestUploadZipPrecheckIsBounded: Der Vorabcheck liest ein ZIP mit mehr
// Einträgen als die Grenze nicht ein (400k Einträge ≈ 94 MB Verzeichnis) —
// dann entscheidet das Backend. Eine zu große Datei prüft er gar nicht erst.
func TestUploadZipPrecheckIsBounded(t *testing.T) {
	old := zipCheckMaxEntries
	zipCheckMaxEntries = 3
	defer func() { zipCheckMaxEntries = old }()
	root := dataroom(t, map[string]string{})
	writeZip(t, filepath.Join(root, "viele.zip"), map[string]string{"a": "", "b": "", "c": "", "d": ""})
	h := newHarness(t)
	h.respond = `[]`
	if code, _, stderr := h.run("documents", "upload", filepath.Join(root, "viele.zip")); code != 0 || h.last == nil {
		t.Errorf("über der Eintragsgrenze ungeprüft senden erwartet, got %d (%s)", code, stderr)
	}
	h2 := newHarness(t)
	code, _, stderr := h2.run("documents", "upload", filepath.Join(root, "viele.zip"), "--max-file-mb", "0.0000001")
	if code != 1 || h2.last != nil {
		t.Fatalf("zu groß: Exit 1 ohne Request erwartet, got %d (%s)", code, stderr)
	}
	if line := errorLine(t, stderr); line["code"] != "FILE_TOO_LARGE" {
		t.Errorf("Größe vor dem ZIP-Check: FILE_TOO_LARGE erwartet, got %v", line)
	}
}

// TestUploadSingleReportBatchFailureKeepsReport: Ein Ordner mit einem
// einzigen Stapel, der nach dem Senden scheitert (503), endete mit nackter
// Fehlerzeile — ohne Bericht, ohne may_exist.
func TestUploadSingleReportBatchFailureKeepsReport(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	h := newHarness(t)
	h.status = 503
	h.respond = `{"message":"Service Unavailable"}`
	code, stdout, stderr := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
	}
	var out struct {
		Failed []map[string]any `json:"failed"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("Bericht auf stdout erwartet: %v (%q)", err, stdout)
	}
	if len(out.Failed) != 2 || out.Failed[0]["may_exist"] != true {
		t.Errorf("beide Dateien mit may_exist erwartet, got %v", out.Failed)
	}
}

// TestUploadUnreadableSuccessBodyMayExist: Ein 2xx mit unlesbarem Body heißt:
// Der Server hat angenommen — ob er gespeichert hat, ist offen.
func TestUploadUnreadableSuccessBodyMayExist(t *testing.T) {
	root := dataroom(t, map[string]string{"a.pdf": "A", "b.pdf": "B"})
	h := newHarness(t)
	h.respond = `{"documents":[kaputt`
	code, stdout, _ := h.run("documents", "upload", root)
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	var out struct {
		Failed []map[string]any `json:"failed"`
	}
	_ = json.Unmarshal([]byte(stdout), &out)
	if len(out.Failed) != 2 || out.Failed[0]["may_exist"] != true {
		t.Errorf("may_exist an beiden Dateien erwartet, got %v", out.Failed)
	}
}

// TestUploadZipPrecheckIgnoresForgedEntryCount: Die Eintragszahl im
// End-of-Central-Directory ist nur 16 Bit breit. 65.536 Einträge mit Zahl 0
// passierten die Grenze, und archive/zip las sie alle ein (Codex) — größere
// Varianten kosten Gigabytes. Über zipCheckMaxBytes liest der Check nichts.
func TestUploadZipPrecheckIgnoresForgedEntryCount(t *testing.T) {
	old := zipCheckMaxBytes
	zipCheckMaxBytes = 1 << 20
	defer func() { zipCheckMaxBytes = old }()
	root := dataroom(t, map[string]string{})
	var dir bytes.Buffer
	const entries = 1 << 16
	for i := 0; i < entries; i++ {
		name := fmt.Sprintf("%05d", i)
		header := make([]byte, 46)
		copy(header, "PK\x01\x02")
		binary.LittleEndian.PutUint16(header[28:], uint16(len(name)))
		dir.Write(header)
		dir.WriteString(name)
	}
	eocd := make([]byte, 22)
	copy(eocd, "PK\x05\x06")
	binary.LittleEndian.PutUint32(eocd[12:], uint32(dir.Len())) // Größe; Zahl und Offset bleiben 0
	dir.Write(eocd)
	if err := os.WriteFile(filepath.Join(root, "gefaelscht.zip"), dir.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.respond = `[]`
	if code, _, stderr := h.run("documents", "upload", filepath.Join(root, "gefaelscht.zip")); code != 0 || h.last == nil {
		t.Errorf("über der Größengrenze ungeprüft senden erwartet, got %d (%s)", code, stderr)
	}
}

// TestUploadZipPrecheckKeepsRealArchives: Ein eigener Parser des Verzeichnisses
// hielt echte ZIPs für leer (vorangestellte Daten, EOCD-Signatur im Kommentar)
// und stürzte bei Namen über 256 Byte ab (Codex). Diese Fälle laufen über
// archive/zip und werden gesendet.
func TestUploadZipPrecheckKeepsRealArchives(t *testing.T) {
	root := dataroom(t, map[string]string{})
	langerName := strings.Repeat("a", 300) + ".pdf"
	writeZip(t, filepath.Join(root, "lang.zip"), map[string]string{langerName: "%PDF-1 Inhalt"})
	writeZip(t, filepath.Join(root, "basis.zip"), map[string]string{"vertrag.pdf": "%PDF-1 Inhalt"})
	basis, err := os.ReadFile(filepath.Join(root, "basis.zip"))
	if err != nil {
		t.Fatal(err)
	}
	// Selbstentpackend: Daten vor dem Archiv, mit einer Verzeichnis-Signatur darin.
	sfx := append([]byte("MZ\x00\x00PK\x01\x02"+strings.Repeat("\x00", 120)), basis...)
	if err := os.WriteFile(filepath.Join(root, "sfx.zip"), sfx, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lang.zip", "sfx.zip"} {
		h := newHarness(t)
		h.respond = `[]`
		if code, _, stderr := h.run("documents", "upload", filepath.Join(root, name)); code != 0 || h.last == nil {
			t.Errorf("%s: senden erwartet, got %d (%s)", name, code, stderr)
		}
	}
	leer := strings.Repeat("b", 300) + ".pdf"
	writeZip(t, filepath.Join(root, "lang-leer.zip"), map[string]string{leer: ""})
	h := newHarness(t)
	if code, _, _ := h.run("documents", "upload", filepath.Join(root, "lang-leer.zip")); code != 1 || h.last != nil {
		t.Errorf("leeres ZIP mit langem Namen: Exit 1 ohne Request erwartet, got %d", code)
	}
}

// TestUploadZipPrecheckAmbiguousEndIsSent: Ein Archiv-Kommentar mit einer
// zweiten, leeren EOCD-Signatur ließ archive/zip ein Verzeichnis ohne Einträge
// wählen. Das CLI hielt das ZIP für leer und schickte den Mietvertrag nicht
// (Codex). Mehrdeutig heißt: das Backend urteilt.
func TestUploadZipPrecheckAmbiguousEndIsSent(t *testing.T) {
	root := dataroom(t, map[string]string{})
	f, err := os.Create(filepath.Join(root, "kommentar.zip"))
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	part, _ := w.Create("vertrag.pdf")
	_, _ = part.Write([]byte("%PDF-1 Inhalt"))
	if err := w.SetComment("PK\x05\x06" + strings.Repeat("\x00", 18)); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_ = f.Close()
	h := newHarness(t)
	h.respond = `[]`
	if code, _, stderr := h.run("documents", "upload", filepath.Join(root, "kommentar.zip")); code != 0 || h.last == nil {
		t.Errorf("senden erwartet, got %d (%s)", code, stderr)
	}
}
