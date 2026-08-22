// Command immojump ist das CLI für die immoJUMP-API.
//
// Die eigentliche Arbeit steckt in internal/cli; hier wird nur die Umgebung
// eingehängt und der Exit-Code weitergereicht.
package main

import (
	"os"

	"github.com/immoJUMP/immojump-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Options{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
	}))
}
