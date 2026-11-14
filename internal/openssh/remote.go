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
	"sync"
)

// ErrNoPOSIXShell is returned by Run when the host logged in but has no
// POSIX sh to run the script, as on a Windows host without one.
var ErrNoPOSIXShell = errors.New("the host has no POSIX sh")

// posixMarker is the line a script's sh prints first, proving that it ran.
const posixMarker = "sshctl-posix-sh-ok"

// RemoteOptions say how Run's ssh logs in.
type RemoteOptions struct {
	// Identity is the Selected identity, passed with -i; "" lets OpenSSH choose.
	Identity string
	// KeyOnly turns off every login but public keys and every prompt, so
	// ssh succeeds only if a key alone logs in.
	KeyOnly bool
}

// keyOnlyOptions make ssh log in with a public key or fail, without asking.
var keyOnlyOptions = []string{
	"-o", "BatchMode=yes", "-o", "PasswordAuthentication=no",
	"-o", "KbdInteractiveAuthentication=no", "-o", "PreferredAuthentications=publickey", "-o", "IdentitiesOnly=yes",
}

// RemoteArgs returns the ssh command line that runs a POSIX sh script, read
// from ssh's stdin, on alias's host.
func RemoteArgs(alias string, o RemoteOptions) []string {
	argv := []string{"ssh"}
	if o.Identity != "" {
		argv = append(argv, "-i", o.Identity)
	}
	if o.KeyOnly {
		argv = append(argv, keyOnlyOptions...)
	}
	return append(argv, alias, "sh", "-s")
}

// command returns the process for argv through the Exec seam.
func (c *Client) command(ctx context.Context, argv []string) *exec.Cmd {
	if c.Exec != nil {
		return c.Exec(ctx, argv)
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// Run runs script with POSIX sh on alias's host over ssh (RemoteArgs). The
// script goes to ssh's stdin, so ssh still asks for a password on the
// terminal. Its output goes to stdout and stderr as it comes.
//
// Run returns ErrNoPOSIXShell when the host has no sh, and an error for
// which ConnectFailed is true when ssh could not log in.
func (c *Client) Run(ctx context.Context, alias string, o RemoteOptions, script string, stdout, stderr io.Writer) error {
	cmd := c.command(ctx, RemoteArgs(alias, o))
	cmd.Stdin = bytes.NewBufferString("printf '%s\\n' " + posixMarker + "\n" + script)
	if stderr == nil {
		stderr = os.Stderr
	}
	if stdout == stderr {
		// One writer gets both streams from two goroutines: take turns.
		w := &lockedWriter{w: stdout}
		stdout, stderr = w, w
	}
	m := &markerWriter{w: stdout}
	cmd.Stdout = m
	cmd.Stderr = struct{ io.Writer }{stderr} // hide io.ReaderFrom, which could hold the writer for a whole copy
	err := cmd.Run()
	m.flush()
	switch {
	case ConnectFailed(err):
		return fmt.Errorf("ssh %s could not log in: %w", alias, err)
	case !m.seen:
		return fmt.Errorf("%s: %w", alias, ErrNoPOSIXShell)
	case err != nil:
		return fmt.Errorf("the script on %s failed: %w", alias, err)
	}
	return nil
}

// ConnectFailed reports whether err is ssh's own failure, such as a refused
// login, rather than the remote command's: ssh exits with 255 then.
func ConnectFailed(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 255
}

// markerWriter passes output to w, holding it line by line until the
// posixMarker line, which it drops, and then passing it straight through.
type markerWriter struct {
	w    io.Writer
	seen bool
	line []byte
}

func (m *markerWriter) Write(p []byte) (int, error) {
	if m.seen {
		return m.write(p)
	}
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			m.line = append(m.line, p...)
			return n, nil
		}
		line := append(m.line, p[:i+1]...)
		m.line = nil
		p = p[i+1:]
		if string(bytes.TrimRight(line, "\r\n")) == posixMarker {
			m.seen = true
			_, err := m.write(p)
			return n, err
		}
		if _, err := m.write(line); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (m *markerWriter) write(p []byte) (int, error) {
	if m.w == nil || len(p) == 0 {
		return len(p), nil
	}
	return m.w.Write(p)
}

func (m *markerWriter) flush() {
	m.write(m.line)
	m.line = nil
}

// lockedWriter lets two goroutines write to w in turn.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// InstallKey adds the public key pub (one authorized_keys line) to the
// remote user's ~/.ssh/authorized_keys on alias's host, over Run with
// identity, unless a line with the same key is already there. It creates
// ~/.ssh as needed and makes it and the file private to the user (700, 600).
// ssh asks for a password on the terminal if no key logs in yet.
func (c *Client) InstallKey(ctx context.Context, alias, identity, pub string, stderr io.Writer) (added bool, err error) {
	script, err := installKeyScript(pub)
	if err != nil {
		return false, err
	}
	var out bytes.Buffer
	if err := c.Run(ctx, alias, RemoteOptions{Identity: identity}, script, &out, stderr); err != nil {
		return false, err
	}
	switch strings.TrimSpace(out.String()) {
	case "added":
		return true, nil
	case "present":
		return false, nil
	}
	return false, fmt.Errorf("unexpected reply from %s: %q", alias, out.String())
}

// installKeyScript is InstallKey's sh script for pub.
func installKeyScript(pub string) (string, error) {
	fields := strings.Fields(pub)
	if len(fields) < 2 || !validKeyType(fields[0]) || !validBase64(fields[1]) || strings.ContainsAny(pub, "\r\n") {
		return "", fmt.Errorf("%q is not an OpenSSH public key", pub)
	}
	line := strings.Join(fields, " ")
	return `set -e
umask 077
d="$HOME/.ssh"
f="$d/authorized_keys"
mkdir -p "$d"
chmod 700 "$d"
touch "$f"
chmod 600 "$f"
if awk -v k=` + shellQuote(fields[1]) + ` '{ for (i = 1; i <= NF; i++) if ($i == k) found = 1 } END { exit !found }' "$f"; then
	echo present
	exit 0
fi
if [ -s "$f" ] && [ -n "$(tail -c 1 "$f")" ]; then
	echo >>"$f"
fi
printf '%s\n' ` + shellQuote(line) + ` >>"$f"
echo added
`, nil
}

func validKeyType(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-.@") == ""
}

func validBase64(s string) bool {
	return len(s) >= 4 && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=") == ""
}

// shellQuote quotes s as one POSIX sh word.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// CheckKeyOnly proves that identity alone logs in to alias's host: ssh with
// KeyOnly, so it can neither ask for a password nor fall back to one.
func (c *Client) CheckKeyOnly(ctx context.Context, alias, identity string, stderr io.Writer) error {
	return c.Run(ctx, alias, RemoteOptions{Identity: identity, KeyOnly: true}, "", io.Discard, stderr)
}

// Resolve returns the HostName, User and Port that ssh uses for alias, as
// "ssh -G" prints them after reading all of ssh_config. It does not connect.
func (c *Client) Resolve(ctx context.Context, alias string) (Endpoint, error) {
	cmd := c.command(ctx, []string{"ssh", "-G", alias})
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		return Endpoint{}, fmt.Errorf("ssh -G %s: %s: %w", alias, strings.TrimSpace(errOut.String()), err)
	}
	var e Endpoint
	for _, line := range strings.Split(string(out), "\n") {
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
