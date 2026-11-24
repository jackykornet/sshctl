package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// connectTree is an app over an ssh_config with web01 and an alias named
// like the list command, with a Selected identity. Its ssh records each
// command line instead of running.
func connectTree(t *testing.T) (*app, *bytes.Buffer, *[][]string, string) {
	t.Helper()
	a, out := cliTree(t, "Host web01\n  HostName 10.0.0.1\nHost list\n  HostName 10.0.0.2\n", config.New(), "")
	key := filepath.Join(t.TempDir(), "id_work")
	if err := os.WriteFile(key, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.catalog().SetIdentity(key); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	a.runSSH = func(_ context.Context, argv []string, _ io.Writer) error {
		calls = append(calls, argv)
		return nil
	}
	return a, out, &calls, key
}

// A first word that is not a built-in command is an alias, and -c
// (--connect) always names one (ADR-0011).
func TestABareAliasOrConnectOptionConnects(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		alias string
	}{
		{[]string{"web01"}, "web01"},
		{[]string{"-c", "web01"}, "web01"},
		{[]string{"--connect", "web01"}, "web01"},
		{[]string{"--connect=web01"}, "web01"},
		{[]string{"-c", "list"}, "list"},
		{[]string{"--connect=list"}, "list"},
		{[]string{"--", "web01"}, "web01"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			a, out, calls, key := connectTree(t)
			if err := a.execute(tc.args); err != nil {
				t.Fatal(err)
			}
			want := [][]string{{"ssh", "-i", key, tc.alias}}
			if !slices.EqualFunc(*calls, want, slices.Equal) {
				t.Errorf("ssh ran %v, want %v", *calls, want)
			}
			if out.Len() != 0 {
				t.Errorf("output %q, want none", out)
			}
			if got := load(t, a).History; !slices.Equal(got, []string{tc.alias}) {
				t.Errorf("History = %v, want [%s]", got, tc.alias)
			}
		})
	}
}

// A built-in command wins over an alias of the same name, which list marks,
// pointing to -c.
func TestACommandNameStaysACommand(t *testing.T) {
	a, out, calls, _ := connectTree(t)
	if err := a.execute([]string{"list"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Errorf("sshctl list ran ssh %v", *calls)
	}
	want := "" +
		"web01\n" +
		"list                     (sshctl -c list)\n"
	if out.String() != want {
		t.Errorf("list:\n%s\nwant:\n%s", out, want)
	}
}

func TestConnectRefusesAWordThatIsNeitherCommandNorConnection(t *testing.T) {
	a, _, calls, _ := connectTree(t)
	for _, args := range [][]string{{"lsit"}, {"connect", "web01"}} {
		err := a.execute(args)
		if err == nil || !strings.Contains(err.Error(), "sshctl --help") || !strings.Contains(err.Error(), "sshctl -c "+args[0]) {
			t.Errorf("sshctl %s: err = %v, want one naming --help and -c", strings.Join(args, " "), err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("ssh ran %v", *calls)
	}
	// -c connects to any name, as ssh does.
	if err := a.execute([]string{"-c", "lsit"}); err != nil || len(*calls) != 1 {
		t.Errorf("sshctl -c lsit: err %v, ssh ran %v", err, *calls)
	}
}

func TestConnectUsage(t *testing.T) {
	a, _, calls, _ := connectTree(t)
	for _, args := range [][]string{
		{"-c"}, {"web01", "ls"}, {"-c", "web01", "web01"}, {"web01", "--yes"},
		{"web01", "--claude", "--", "ls"}, {"-c", "web01", "-c", "list"},
	} {
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), "usage: sshctl [-c] <alias>") {
			t.Errorf("sshctl %s: err = %v, want the connect usage", strings.Join(args, " "), err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("ssh ran %v", *calls)
	}
}

func TestConnectClaude(t *testing.T) {
	a, _, calls, key := connectTree(t)
	if err := a.execute([]string{"web01", "--claude"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ssh", "-t", "-i", key, "web01", "claude"}; !slices.Equal((*calls)[0], want) {
		t.Errorf("ssh ran %v, want %v", (*calls)[0], want)
	}
}

// sshctl <alias> -- <command...> runs the command on the host with the
// Selected identity.
func TestRemoteCommandIsPassedToSSH(t *testing.T) {
	for _, args := range [][]string{
		{"web01", "--", "ls", "-l", "/tmp"},
		{"-c", "web01", "--", "ls", "-l", "/tmp"},
		{"--connect=web01", "--", "ls", "-l", "/tmp"},
	} {
		a, _, calls, key := connectTree(t)
		if err := a.execute(args); err != nil {
			t.Fatal(err)
		}
		want := [][]string{{"ssh", "-i", key, "web01", "ls", "-l", "/tmp"}}
		if !slices.EqualFunc(*calls, want, slices.Equal) {
			t.Errorf("sshctl %s: ssh ran %v, want %v", strings.Join(args, " "), *calls, want)
		}
	}
}

func TestVersion(t *testing.T) {
	fakeHome(t)
	var out bytes.Buffer
	if err := run([]string{"--version"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "sshctl dev\n" {
		t.Errorf("--version = %q, want sshctl dev", out)
	}
}

// script is a remote command for sh on POSIX and for cmd.exe on Windows.
type script struct{ sh, cmd []string }

// sshctl exits with the remote command's exit code. ssh is replaced by a
// local shell running the command it would send.
func TestRemoteCommandExitCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote script
		code   int
	}{
		{"false", script{[]string{"false"}, []string{"exit", "1"}}, 1},
		{"exit 3", script{[]string{"exit", "3"}, []string{"exit", "3"}}, 3},
		{"true", script{[]string{"true"}, []string{"exit", "0"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _, _ := connectTree(t)
			remote := tc.remote.sh
			if runtime.GOOS == "windows" {
				remote = tc.remote.cmd
			}
			a.runSSH = func(ctx context.Context, argv []string, _ io.Writer) error {
				i := slices.Index(argv, "web01")
				command := strings.Join(argv[i+1:], " ")
				if runtime.GOOS == "windows" {
					return exec.CommandContext(ctx, "cmd", "/c", command).Run()
				}
				return exec.CommandContext(ctx, "sh", "-c", command).Run()
			}
			err := a.execute(append([]string{"web01", "--"}, remote...))
			if got := exitCode(err); got != tc.code {
				t.Errorf("exit code %d (%v), want %d", got, err, tc.code)
			}
		})
	}
}
