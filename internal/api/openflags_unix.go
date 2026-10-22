//go:build unix

package api

import (
	"os"
	"syscall"
)

// nonblockFlag lässt open(2) an einer Named Pipe ohne Schreiber sofort
// zurückkehren, statt zu blockieren — die Typprüfung danach lehnt sie ab.
const nonblockFlag = syscall.O_NONBLOCK

// clearNonblock schaltet den Deskriptor einer geprüften normalen Datei
// zurück in den blockierenden Modus.
func clearNonblock(f *os.File) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if err := conn.Control(func(fd uintptr) { setErr = syscall.SetNonblock(int(fd), false) }); err != nil {
		return err
	}
	return setErr
}
