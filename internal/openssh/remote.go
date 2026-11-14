package openssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	return posixResult(run(ctx, RemoteArgs(alias, key, posixProbe), &out), out.String())
}

// posixResult classifies how a script that starts with posixProbe ended,
// given run's error and output: ssh's own error, or one that kept ssh from
// starting, as it is; no posixAnswer as ErrNoPOSIX; otherwise the script's
// error, or nil.
func posixResult(err error, output string) error {
	var exit interface{ ExitCode() int }
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() == sshFailed) {
		return err
	}
	if !strings.Contains(output, posixAnswer) {
		return ErrNoPOSIX
	}
	return err
}

// keyOnlyOptions make ssh log in with a public key or fail, without asking.
var keyOnlyOptions = []string{
	"-o", "BatchMode=yes", "-o", "PasswordAuthentication=no",
	"-o", "KbdInteractiveAuthentication=no", "-o", "PreferredAuthentications=publickey", "-o", "IdentitiesOnly=yes",
}

// CheckKeyOnly proves that key alone logs in to alias's host: ssh may
// neither ask anything nor use any login but that public key. It runs
// "exit 0", which every login shell understands.
func CheckKeyOnly(ctx context.Context, run Runner, alias, key string) error {
	argv := []string{"ssh"}
	if key != "" {
		argv = append(argv, "-i", key)
	}
	argv = append(append(argv, keyOnlyOptions...), alias, "exit 0")
	return run(ctx, argv, io.Discard)
}

// Resolve returns the HostName, User and Port that ssh uses for alias, as
// "ssh -G" prints them after reading all of ssh_config. It does not connect.
func Resolve(ctx context.Context, run Runner, alias string) (Endpoint, error) {
	var out bytes.Buffer
	if err := run(ctx, []string{"ssh", "-G", alias}, &out); err != nil {
		return Endpoint{}, fmt.Errorf("ssh -G %s: %w", alias, err)
	}
	var e Endpoint
	for _, line := range strings.Split(out.String(), "\n") {
		keyword, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch keyword {
		case "hostname":
			e.HostName = value
		case "user":
			e.User = value
		case "port":
			e.Port = value
		}
	}
	return e, nil
}

// InstallKey adds the public key pub (an authorized_keys line) to the remote
// user's ~/.ssh/authorized_keys on alias's host, logging in with key, unless
// a line already holds the same key. It creates ~/.ssh as needed and makes
// it and the file private to that user (700 and 600). ssh asks for a
// password on the terminal when no key logs in yet. The host's POSIX sh is
// checked in the same login, so the password is asked once: without it,
// InstallKey returns ErrNoPOSIX and changes nothing.
func InstallKey(ctx context.Context, run Runner, alias, key, pub string) (added bool, err error) {
	script, err := installKeyScript(pub)
	if err != nil {
		return false, err
	}
	var out bytes.Buffer
	err = posixResult(run(ctx, RemoteArgs(alias, key, posixProbe+"\n"+script), &out), out.String())
	if err != nil {
		return false, err
	}
	switch {
	case strings.Contains(out.String(), "\nsshctl-key-added"):
		return true, nil
	case strings.Contains(out.String(), "\nsshctl-key-present"):
		return false, nil
	}
	return false, fmt.Errorf("unexpected reply from %s: %q", alias, out.String())
}

// installKeyScript is InstallKey's sh script for pub. The key's blob is
// compared field by field, so options or another comment on the line do not
// hide it and a longer blob is another key.
func installKeyScript(pub string) (string, error) {
	fields := strings.Fields(pub)
	if len(fields) < 2 || !validKeyType(fields[0]) || !validBase64(fields[1]) {
		return "", fmt.Errorf("%q is not an OpenSSH public key", pub)
	}
	return `set -e
umask 077
d="$HOME/.ssh"
f="$d/authorized_keys"
mkdir -p "$d"
chmod 700 "$d"
touch "$f"
chmod 600 "$f"
if awk -v k=` + ShellQuote(fields[1]) + ` '{ for (i = 1; i <= NF; i++) if ($i == k) found = 1 } END { exit !found }' "$f"; then
	echo sshctl-key-present
	exit 0
fi
if [ -s "$f" ] && [ -n "$(tail -c 1 "$f")" ]; then
	echo >>"$f"
fi
printf '%s\n' ` + ShellQuote(strings.Join(fields, " ")) + ` >>"$f"
echo sshctl-key-added
`, nil
}

func validKeyType(s string) bool {
	return strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-.@") == ""
}

func validBase64(s string) bool {
	return len(s) >= 4 && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=") == ""
}
