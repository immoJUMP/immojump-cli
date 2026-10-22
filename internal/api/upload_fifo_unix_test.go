//go:build unix

package api

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestUploadDoesNotBlockOnSwappedFIFO: Wird die geprüfte Datei vor dem
// Öffnen durch eine Named Pipe ersetzt, blockierte os.Open ohne Schreiber
// für immer — noch vor der Typprüfung und außerhalb jedes Timeouts.
func TestUploadDoesNotBlockOnSwappedFIFO(t *testing.T) {
	for _, rooted := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "vertrag.pdf")
		_ = os.WriteFile(path, []byte("%PDF"), 0o600)
		hookBeforeOpen = func(p string) {
			_ = os.Remove(p)
			if err := syscall.Mkfifo(p, 0o600); err != nil {
				t.Errorf("Mkfifo: %v", err)
			}
		}
		file := FilePart{Field: "files[]", Filename: "vertrag.pdf", Path: path}
		if rooted {
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatalf("OpenRoot: %v", err)
			}
			defer func() { _ = root.Close() }()
			file.Root, file.Name = root, "vertrag.pdf"
		}
		srv, calls := scripted(t, []int{200}, nil)
		done := make(chan error, 1)
		go func() {
			_, err := testClient(srv).Do(Request{Method: "POST", Path: "/api/x", Multipart: &Multipart{Files: []FilePart{file}}})
			done <- err
		}()
		select {
		case err := <-done:
			localFileError(t, err, path)
		case <-time.After(5 * time.Second):
			t.Fatalf("rooted=%v: Upload blockiert an der Named Pipe", rooted)
		}
		hookBeforeOpen = nil
		if atomic.LoadInt32(calls) != 0 {
			t.Error("an eine Pipe geht nichts raus")
		}
	}
}
