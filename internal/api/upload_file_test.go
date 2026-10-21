package api

import (
	"errors"
	"os"
	"path/filepath"
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
