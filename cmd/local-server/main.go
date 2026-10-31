// Command local-server shares the host machine's AI inference with invited
// consumers. This is the scaffold entry point; features arrive with their
// tickets.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("local-server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "local-server", version)
		return 0
	}
	fmt.Fprintln(stderr, "local-server: nothing to run yet; use --version")
	return 2
}
