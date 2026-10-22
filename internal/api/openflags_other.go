//go:build !unix

package api

import "os"

// nonblockFlag gibt es nur auf Unix; ohne Named Pipes ist nichts zu tun.
const nonblockFlag = 0

func clearNonblock(*os.File) error { return nil }
