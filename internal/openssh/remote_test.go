package openssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"slices"
	"testing"
)

func TestRemoteArgsRunsTheScriptWithSh(t *testing.T) {
	for _, tc := range []struct {
		key, script string
		want        []string
	}{
		{"", "echo hi", []string{"ssh", "web", "sh -c 'echo hi'"}},
		{"/k/id", "echo 'it'", []string{"ssh", "-i", "/k/id", "web", `sh -c 'echo '\''it'\'''`}},
	} {
		if got := RemoteArgs("web", tc.key, tc.script); !slices.Equal(got, tc.want) {
			t.Errorf("RemoteArgs(%q) = %q, want %q", tc.script, got, tc.want)
		}
	}
}

// TestRemoteArgsQuotingSurvivesALoginShell runs the command ssh would hand
// the remote login shell through a local sh.
func TestRemoteArgsQuotingSurvivesALoginShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}
	argv := RemoteArgs("web", "", `printf '%s|' "a b" 'c'"'"'d' $((1+1))`)
	out, err := exec.Command("sh", "-c", argv[len(argv)-1]).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "a b|c'd|2|"; got != want {
		t.Errorf("remote output = %q, want %q", got, want)
	}
}

// fakeSSH is a Runner that records argv and answers with output and err.
type fakeSSH struct {
	calls  [][]string
	output string
	err    error
}

func (f *fakeSSH) run(_ context.Context, argv []string, out io.Writer) error {
	f.calls = append(f.calls, argv)
	if out != nil {
		fmt.Fprint(out, f.output)
	}
	return f.err
}

type exitErr int

func (e exitErr) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

func TestCheckPOSIX(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  string
		err     error
		wantErr error
	}{
		{"sh answers", "sshctl-posix-2\n", nil, nil},
		{"no sh", "sh: command not found\n", exitErr(127), ErrNoPOSIX},
		{"not POSIX", "sshctl-posix-$((1+1))\n", nil, ErrNoPOSIX},
		{"ssh fails", "", exitErr(255), exitErr(255)},
		{"no ssh", "", exec.ErrNotFound, exec.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSSH{output: tc.output, err: tc.err}
			err := CheckPOSIX(context.Background(), f.run, "web", "/k/id")
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			want := []string{"ssh", "-i", "/k/id", "web", "sh -c 'echo sshctl-posix-$((1+1))'"}
			if len(f.calls) != 1 || !slices.Equal(f.calls[0], want) {
				t.Errorf("ran %q, want %q", f.calls, want)
			}
		})
	}
}
