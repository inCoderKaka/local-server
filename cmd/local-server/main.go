// Command local-server shares the host machine's AI inference with invited
// consumers. This is the scaffold entry point; features arrive with their
// tickets.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	// IANA zone data for host.quiet_hours.time_zone and policy windows on machines without a system zoneinfo database (Windows, minimal Linux).
	_ "time/tzdata"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("local-server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "local-server", version)
		return 0
	}
	fmt.Fprintln(stderr, "local-server: nothing to run yet; use --version")
	return 2
}
