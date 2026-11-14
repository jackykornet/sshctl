package openssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner runs an ssh command line. Its standard output goes to out, or to
// the terminal when out is nil; standard input and error are the terminal.
// Tests replace it so they never start the real ssh.
type Runner func(ctx context.Context, argv []string, out io.Writer) error

// Run is the Runner that starts the real ssh.
func Run(ctx context.Context, argv []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if out != nil {
		cmd.Stdout = out
	}
	return cmd.Run()
}

// RemoteArgs returns the ssh command line that runs script with sh on alias's
// host. ssh hands its command to the remote user's login shell, so the script
// is quoted once for that shell and then run by sh whatever the login shell
// is. A non-empty key is the Selected identity.
func RemoteArgs(alias, key, script string) []string {
	argv := []string{"ssh"}
	if key != "" {
		argv = append(argv, "-i", key)
	}
	return append(argv, alias, "sh -c "+ShellQuote(script))
}

// ShellQuote quotes s as one word for a POSIX shell.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ErrNoPOSIX is returned by CheckPOSIX when the host has no POSIX sh.
var ErrNoPOSIX = errors.New("the host has no POSIX sh")

// posixProbe prints posixAnswer only when a POSIX sh runs it.
const (
	posixProbe  = "echo sshctl-posix-$((1+1))"
	posixAnswer = "sshctl-posix-2"
)

// sshFailed is the exit code ssh uses for its own errors, such as a failed
// connection or login.
const sshFailed = 255

// CheckPOSIX makes sure alias's host runs POSIX sh scripts, so a remote
// action can fail before it changes anything. An ssh error is returned as it
// is, as is one that keeps ssh from starting; any other failure to answer
// is ErrNoPOSIX.
func CheckPOSIX(ctx context.Context, run Runner, alias, key string) error {
	var out bytes.Buffer
	err := run(ctx, RemoteArgs(alias, key, posixProbe), &out)
	var exit interface{ ExitCode() int }
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() == sshFailed) {
		return err
	}
	if err == nil && strings.Contains(out.String(), posixAnswer) {
		return nil
	}
	return ErrNoPOSIX
}
