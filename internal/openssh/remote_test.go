package openssh

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
)

// fakeRemote is a stand-in for ssh: sh runs on POSIX, cmd on Windows, in
// place of the whole ssh command line, which is recorded in argv.
type fakeRemote struct{ sh, cmd string }

func (f fakeRemote) client(argv *[]string) *Client {
	return &Client{Exec: func(ctx context.Context, args []string) *exec.Cmd {
		*argv = args
		if runtime.GOOS == "windows" {
			return exec.CommandContext(ctx, "cmd.exe", "/d", "/q", "/s", "/c", f.cmd)
		}
		return exec.CommandContext(ctx, "sh", "-c", f.sh)
	}}
}

func TestRunPassesOutputWithoutTheShellMarker(t *testing.T) {
	// The fake host reads the script and answers as POSIX sh would.
	var argv []string
	c := fakeRemote{
		sh:  `cat >/dev/null; echo motd; echo ` + posixMarker + `; echo hello`,
		cmd: `echo motd& echo ` + posixMarker + `& echo hello`,
	}.client(&argv)
	var out bytes.Buffer
	if err := c.Run(context.Background(), "web", RemoteOptions{Identity: "/k/id"}, "echo hello", &out, &out); err != nil {
		t.Fatal(err)
	}
	if got := normalize(out.String()); got != "motd\nhello\n" {
		t.Errorf("output = %q (raw %q), want motd and hello without the marker", got, out.String())
	}
	if want := []string{"ssh", "-i", "/k/id", "web", "sh", "-s"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestRunReportsAHostWithoutPOSIXShell(t *testing.T) {
	var argv []string
	c := fakeRemote{
		sh:  `echo "sh: command not found" >&2; exit 127`,
		cmd: `echo 'sh' is not recognized as an internal or external command 1>&2& exit /b 1`,
	}.client(&argv)
	var out bytes.Buffer
	err := c.Run(context.Background(), "win", RemoteOptions{}, "true", &out, &out)
	if !errors.Is(err, ErrNoPOSIXShell) {
		t.Fatalf("err = %v, want ErrNoPOSIXShell", err)
	}
}

func TestRunReportsAFailedConnectionAsSuch(t *testing.T) {
	var argv []string
	c := fakeRemote{
		sh:  `echo "Permission denied (publickey)." >&2; exit 255`,
		cmd: `echo Permission denied (publickey).1>&2& exit /b 255`,
	}.client(&argv)
	var out bytes.Buffer
	err := c.Run(context.Background(), "web", RemoteOptions{Identity: "/k/id", KeyOnly: true}, "", &out, &out)
	if err == nil || errors.Is(err, ErrNoPOSIXShell) {
		t.Fatalf("err = %v, want a connection failure", err)
	}
	if !ConnectFailed(err) {
		t.Errorf("ConnectFailed(%v) = false, want true", err)
	}
	want := []string{"ssh", "-i", "/k/id", "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no", "-o", "PreferredAuthentications=publickey", "-o", "IdentitiesOnly=yes",
		"web", "sh", "-s"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q\nwant %q", argv, want)
	}
}

// normalize turns Windows line endings into "\n".
func normalize(s string) string { return string(bytes.ReplaceAll([]byte(s), []byte("\r\n"), []byte("\n"))) }
