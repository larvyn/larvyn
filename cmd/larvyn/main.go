// Command larvyn is the larvyn CLI (internal/cli) as a console program, for
// Windows, where the app itself is a GUI program that can't print to a
// terminal. The app carries it and writes it to its state folder at start
// (internal/clibin).
package main

import (
	"os"

	"larvyn/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
