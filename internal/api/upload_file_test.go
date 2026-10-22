package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func uploadRequest(path string) Request {
	return Request{
		Method: "POST", Path: "/api/documents/documents/bulk-upload",
		Multipart: &Multipart{Files: []FilePart{{Field: "files[]", Filename: filepath.Base(path), Path: path}}},
	}
}

func localFileError(t *testing.T, err error, path string) {
	t.Helper()
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "LOCAL_FILE" {
		t.Fatalf("LOCAL_FILE-Fehler erwartet, got %#v", err)
	}
	if apiErr.Details["file"] != path {
		t.Errorf("Fehler soll die Datei nennen, got %v", apiErr.Details)
	}
}

func TestUploadRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "geheim.txt")
	link := filepath.Join(dir, "vertrag.pdf")
	_ = os.WriteFile(target, []byte("x"), 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Symlinks nicht verfügbar: %v", err)
	}
	srv, calls := scripted(t, []int{200}, nil)
	_, err := testClient(srv).Do(uploadRequest(link))
	localFileError(t, err, link)
	if atomic.LoadInt32(calls) != 0 {
		t.Error("ohne gültige Datei kein Request")
	}
}

// TestUploadDetectsSwapBetweenCheckAndOpen: Wird die geprüfte Datei
// zwischen Lstat und Open durch einen Symlink ersetzt, darf der Inhalt des
// Ziels nicht hochgeladen werden.
func TestUploadDetectsSwapBetweenCheckAndOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vertrag.pdf")
	secret := filepath.Join(dir, "geheim.txt")
	_ = os.WriteFile(path, []byte("%PDF"), 0o600)
	_ = os.WriteFile(secret, []byte("GEHEIM"), 0o600)
	hookBeforeOpen = func(p string) {
		_ = os.Remove(p)
		_ = os.Symlink(secret, p)
	}
	defer func() { hookBeforeOpen = nil }()
	srv, calls := scripted(t, []int{200}, nil)
	_, err := testClient(srv).Do(uploadRequest(path))
	localFileError(t, err, path)
	if atomic.LoadInt32(calls) != 0 {
		t.Error("nach dem Tausch kein Request")
	}
}

// TestUploadFailsWhenFileGrowsWhileSending: Die Länge stammt aus dem Fstat
// des geöffneten Deskriptors; wächst die Datei danach, ist der Upload
// ungültig statt still abgeschnitten.
func TestUploadFailsWhenFileGrowsWhileSending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan.pdf")
	_ = os.WriteFile(path, []byte("%PDF-1.4"), 0o600)
	hookAfterStat = func(p string) {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		_, _ = f.Write([]byte(" mehr"))
		_ = f.Close()
	}
	defer func() { hookAfterStat = nil }()
	srv, _ := scripted(t, []int{200}, nil)
	_, err := testClient(srv).Do(uploadRequest(path))
	if err == nil {
		t.Fatal("Fehler erwartet, wenn die Datei beim Senden wächst")
	}
	if ExitCodeFor(err) == 8 {
		t.Errorf("lokaler Fehler, kein Exit 8 (Retry hilft nicht), got %v", err)
	}
}

// rootedDataroom legt root/sub/vertrag.pdf und außerhalb outside/vertrag.pdf an.
func rootedDataroom(t *testing.T) (dir, outside string, root *os.Root) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "datenraum")
	outside = filepath.Join(base, "fremd")
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o700)
	_ = os.MkdirAll(outside, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "sub", "vertrag.pdf"), []byte("%PDF eigen"), 0o600)
	_ = os.WriteFile(filepath.Join(outside, "vertrag.pdf"), []byte("GEHEIM"), 0o600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return dir, outside, root
}

func rootedRequest(dir string, root *os.Root) Request {
	return Request{
		Method: "POST", Path: "/api/documents/documents/bulk-upload",
		Multipart: &Multipart{Files: []FilePart{{
			Field: "files[]", Filename: "vertrag.pdf", Path: filepath.Join(dir, "sub", "vertrag.pdf"),
			Root: root, Name: filepath.Join("sub", "vertrag.pdf"),
		}}},
	}
}

// swapForSymlink ersetzt dir/sub durch einen Symlink auf outside.
func swapForSymlink(t *testing.T, dir, outside string) {
	t.Helper()
	if err := os.Rename(filepath.Join(dir, "sub"), filepath.Join(dir, "sub.alt")); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "sub")); err != nil {
		t.Skipf("Symlinks nicht verfügbar: %v", err)
	}
}

func TestRootedUploadSendsTheFile(t *testing.T) {
	dir, _, root := rootedDataroom(t)
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		f, _ := r.MultipartForm.File["files[]"][0].Open()
		raw, _ := io.ReadAll(f)
		got = string(raw)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := testClient(srv).Do(rootedRequest(dir, root)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got != "%PDF eigen" {
		t.Errorf("Inhalt der eigenen Datei erwartet, got %q", got)
	}
}

// TestRootedUploadRefusesSwappedParentDirectory: Wird ein Unterordner des
// Datenraums zwischen Aufzählung und Senden durch einen Symlink nach außen
// ersetzt, folgte Lstat dem Eltern-Symlink — os.Root verweigert das.
func TestRootedUploadRefusesSwappedParentDirectory(t *testing.T) {
	dir, outside, root := rootedDataroom(t)
	swapForSymlink(t, dir, outside)
	srv, calls := scripted(t, []int{200}, nil)
	_, err := testClient(srv).Do(rootedRequest(dir, root))
	localFileError(t, err, filepath.Join(dir, "sub", "vertrag.pdf"))
	if atomic.LoadInt32(calls) != 0 {
		t.Error("nichts von außerhalb senden")
	}
}

func TestRootedUploadRefusesSwapBetweenLstatAndOpen(t *testing.T) {
	dir, outside, root := rootedDataroom(t)
	hookBeforeOpen = func(string) { swapForSymlink(t, dir, outside) }
	defer func() { hookBeforeOpen = nil }()
	srv, calls := scripted(t, []int{200}, nil)
	_, err := testClient(srv).Do(rootedRequest(dir, root))
	localFileError(t, err, filepath.Join(dir, "sub", "vertrag.pdf"))
	if atomic.LoadInt32(calls) != 0 {
		t.Error("nichts von außerhalb senden")
	}
}

// TestUploadEnforcesMaxSizeOnOpenedFile: Die Grenze gilt für die Größe des
// geöffneten Deskriptors, nicht nur für die der Aufzählung — eine Datei, die
// bis zum Senden wächst, geht nicht in voller Größe raus.
func TestUploadEnforcesMaxSizeOnOpenedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan.pdf")
	_ = os.WriteFile(path, []byte("%PDF"), 0o600)
	hookBeforeOpen = func(p string) {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		_, _ = f.Write(make([]byte, 2048))
		_ = f.Close()
	}
	defer func() { hookBeforeOpen = nil }()
	srv, calls := scripted(t, []int{200}, nil)
	req := uploadRequest(path)
	req.Multipart.Files[0].MaxSize = 1024
	_, err := testClient(srv).Do(req)
	localFileError(t, err, path)
	if !strings.Contains(err.Error(), "zu groß") {
		t.Errorf("Meldung „zu groß“ erwartet, got %q", err.Error())
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Error("eine zu große Datei geht nicht raus")
	}
}
