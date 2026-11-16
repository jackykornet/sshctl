package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sshctl:", err)
		os.Exit(exitCode(err))
	}
}

// exitCode is the code sshctl exits with after err: a remote command's own
// exit code when err carries one, such as Provision's, and 1 otherwise.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}
	return 1
}
