package main

import (
	"context"
	"errors"
	"fmt"
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

const dotfiles = "https://github.com/jackykornet/dotfiles.git"

func TestProvisionDefaultSetsListsAndUnsetsTheGlobalRepo(t *testing.T) {
	a, out := cliTree(t, "Host web\n", config.New(), "")
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"provision", "default"}, "global: unset\n"},
		{[]string{"provision", "default", dotfiles}, ""},
		{[]string{"provision", "default"}, "global: " + dotfiles + "\n"},
		{[]string{"provision", "default", "none"}, ""},
		{[]string{"provision", "default"}, "global: none (Provision refused)\n"},
		{[]string{"provision", "default", "--unset"}, ""},
		{[]string{"provision", "default"}, "global: unset\n"},
	}
	for _, s := range steps {
		out.Reset()
		stale(a)
		if err := a.execute(s.args); err != nil {
			t.Fatalf("%v: %v", s.args, err)
		}
		if out.String() != s.want {
			t.Errorf("%v printed %q, want %q", s.args, out.String(), s.want)
		}
	}
}

func TestProvisionDefaultIsSavedInTheConfig(t *testing.T) {
	a, _ := cliTree(t, "Host web\n", config.New(), "")
	if err := a.execute([]string{"provision", "default", dotfiles}); err != nil {
		t.Fatal(err)
	}
	if got := load(t, a).Provision.Dotfiles; got != dotfiles {
		t.Errorf("saved dotfiles = %q, want %q", got, dotfiles)
	}
}

func TestProvisionDefaultRefusesBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"provision", "default", "a", "b"},
		{"provision", "default", "--unset", dotfiles},
		{"provision", "default", "--bogus"},
	} {
		a, _ := cliTree(t, "Host web\n", config.New(), "")
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: err = %v, want usage", args, err)
		}
	}
}

// remoteHome is a remote home for running the Provision script under a real
// sh, with only the given fake commands on PATH. Each fake records its
// arguments in $HOME/<name>.args and exits with its code.
type remoteHome struct {
	t         *testing.T
	home, bin string
}

func newRemoteHome(t *testing.T) *remoteHome {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Provision script runs on a POSIX host")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	h := &remoteHome{t: t, home: t.TempDir(), bin: t.TempDir()}
	if err := os.Symlink(sh, filepath.Join(h.bin, "sh")); err != nil {
		t.Fatal(err)
	}
	return h
}

// fake writes an executable at dir/name that records its arguments, prints
// stdout and exits with code.
func (h *remoteHome) fake(dir, name, stdout string, code int) {
	h.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > \"$HOME/%s.args\"\nprintf '%%s' %s\nexit %d\n", name, shellWord(stdout), code)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// installer is what the fake get.chezmoi.io serves: it records its
// arguments as the real one's would be and exits with code.
func installer(code int) string {
	return fmt.Sprintf("printf '%%s\\n' \"$@\" > \"$HOME/installer.args\"; exit %d", code)
}

// run runs the Provision script for repo and returns its exit code and
// standard error.
func (h *remoteHome) run(repo string) (int, string) {
	h.t.Helper()
	cmd := exec.Command("sh", "-c", provisionScript(repo))
	cmd.Env = []string{"HOME=" + h.home, "PATH=" + h.bin}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stderr.String()
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return 0, stderr.String()
}

// args returns what the fake name was called with, or nil if it never ran.
func (h *remoteHome) args(name string) []string {
	b, err := os.ReadFile(filepath.Join(h.home, name+".args"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestProvisionScriptInstallsChezmoiWithCurlOnAFreshHost(t *testing.T) {
	h := newRemoteHome(t)
	h.fake(h.bin, "curl", installer(0), 0)
	h.fake(h.bin, "wget", installer(0), 0)
	if code, stderr := h.run(dotfiles); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got, want := h.args("curl"), []string{"-fsLS", "get.chezmoi.io"}; !slices.Equal(got, want) {
		t.Errorf("curl args = %q, want %q", got, want)
	}
	want := []string{"-b", h.home + "/.local/bin", "init", "--apply", dotfiles}
	if got := h.args("installer"); !slices.Equal(got, want) {
		t.Errorf("installer args = %q, want %q", got, want)
	}
	if h.args("wget") != nil {
		t.Error("wget ran although curl is there")
	}
}

func TestProvisionScriptFallsBackToWget(t *testing.T) {
	h := newRemoteHome(t)
	h.fake(h.bin, "wget", installer(0), 0)
	if code, stderr := h.run(dotfiles); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got, want := h.args("wget"), []string{"-qO-", "get.chezmoi.io"}; !slices.Equal(got, want) {
		t.Errorf("wget args = %q, want %q", got, want)
	}
	want := []string{"-b", h.home + "/.local/bin", "init", "--apply", dotfiles}
	if got := h.args("installer"); !slices.Equal(got, want) {
		t.Errorf("installer args = %q, want %q", got, want)
	}
}

func TestProvisionScriptFailsEarlyWithoutCurlOrWget(t *testing.T) {
	h := newRemoteHome(t)
	code, stderr := h.run(dotfiles)
	if code == 0 || !strings.Contains(stderr, "curl or wget") {
		t.Errorf("exit %d, stderr %q; want a failure naming curl or wget", code, stderr)
	}
}

func TestProvisionScriptStopsWhenTheInstallerCannotBeFetched(t *testing.T) {
	h := newRemoteHome(t)
	h.fake(h.bin, "curl", installer(0), 22)
	if code, _ := h.run(dotfiles); code == 0 {
		t.Error("exit 0, want the download failure")
	}
	if h.args("installer") != nil {
		t.Error("the installer ran after a failed download")
	}
}

func TestProvisionScriptPassesTheApplyExitCodeThrough(t *testing.T) {
	h := newRemoteHome(t)
	h.fake(h.bin, "curl", installer(7), 0)
	if code, _ := h.run(dotfiles); code != 7 {
		t.Errorf("exit %d, want 7 from init --apply", code)
	}
	h = newRemoteHome(t)
	h.fake(h.bin, "chezmoi", "", 9)
	if code, _ := h.run(dotfiles); code != 9 {
		t.Errorf("exit %d, want 9 from update --apply", code)
	}
}

func TestProvisionScriptUpdatesAnInstalledChezmoi(t *testing.T) {
	for _, where := range []string{"PATH", "~/.local/bin"} {
		t.Run(where, func(t *testing.T) {
			h := newRemoteHome(t)
			dir := h.bin
			if where != "PATH" {
				dir = filepath.Join(h.home, ".local", "bin")
			}
			h.fake(dir, "chezmoi", "", 0)
			h.fake(h.bin, "curl", installer(0), 0)
			if code, stderr := h.run(dotfiles); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if got, want := h.args("chezmoi"), []string{"update", "--apply"}; !slices.Equal(got, want) {
				t.Errorf("chezmoi args = %q, want %q", got, want)
			}
			if h.args("curl") != nil || h.args("installer") != nil {
				t.Error("chezmoi was installed again")
			}
		})
	}
}

func TestProvisionScriptKeepsAnOddRepoURLOneArgument(t *testing.T) {
	h := newRemoteHome(t)
	h.fake(h.bin, "curl", installer(0), 0)
	repo := `https://example.com/it's a "repo".git`
	if code, stderr := h.run(repo); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := h.args("installer"); len(got) != 5 || got[4] != repo {
		t.Errorf("installer args = %q, want the repo as one argument", got)
	}
}

// sshRecorder is an openssh.Runner that records every ssh command line. It
// answers the POSIX check as a POSIX host would unless noPOSIX, and fails
// the other commands with fail.
type sshRecorder struct {
	calls   [][]string
	noPOSIX bool
	fail    error
}

func (r *sshRecorder) run(_ context.Context, argv []string, out io.Writer) error {
	r.calls = append(r.calls, argv)
	if strings.Contains(argv[len(argv)-1], "sshctl-posix") {
		if r.noPOSIX {
			return exitCodeError(127)
		}
		fmt.Fprintln(out, "sshctl-posix-2")
		return nil
	}
	return r.fail
}

type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitCodeError) ExitCode() int { return int(e) }

func provisionTree(t *testing.T, repo string) (*app, *sshRecorder) {
	t.Helper()
	cfg := config.New()
	cfg.DefaultKey = "/k/id"
	cfg.Provision.Dotfiles = repo
	a, _ := cliTree(t, "Host web\n", cfg, "")
	r := &sshRecorder{}
	a.runSSH = r.run
	return a, r
}

func TestProvisionChecksForShThenSendsTheScript(t *testing.T) {
	a, r := provisionTree(t, dotfiles)
	if err := a.execute([]string{"provision", "web"}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("ssh ran %d times (%q), want the check and the script", len(r.calls), r.calls)
	}
	for _, argv := range r.calls {
		if got, want := argv[:4], []string{"ssh", "-i", "/k/id", "web"}; !slices.Equal(got, want) {
			t.Errorf("ssh argv starts %q, want %q", got, want)
		}
	}
	// What ssh hands the remote login shell installs chezmoi on a fresh host.
	h := newRemoteHome(t)
	h.fake(h.bin, "curl", installer(0), 0)
	cmd := exec.Command("sh", "-c", r.calls[1][4])
	cmd.Env = []string{"HOME=" + h.home, "PATH=" + h.bin}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("remote command: %v: %s", err, out)
	}
	want := []string{"-b", h.home + "/.local/bin", "init", "--apply", dotfiles}
	if got := h.args("installer"); !slices.Equal(got, want) {
		t.Errorf("installer args = %q, want %q", got, want)
	}
}

func TestProvisionFailsEarly(t *testing.T) {
	for _, tc := range []struct {
		name, repo string
		noPOSIX    bool
		want       string
		calls      int
	}{
		{"no repo", "", false, "sshctl provision default <url>", 0},
		{"refused", config.DotfilesNone, false, "refused", 0},
		{"no POSIX sh", dotfiles, true, "POSIX sh", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, r := provisionTree(t, tc.repo)
			r.noPOSIX = tc.noPOSIX
			err := a.execute([]string{"provision", "web"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
			if len(r.calls) != tc.calls {
				t.Errorf("ssh ran %d times (%q), want %d", len(r.calls), r.calls, tc.calls)
			}
		})
	}
}

func TestProvisionPassesTheRemoteExitCodeThrough(t *testing.T) {
	a, r := provisionTree(t, dotfiles)
	r.fail = exitCodeError(7)
	err := a.execute([]string{"provision", "web"})
	if got := exitCode(err); got != 7 {
		t.Errorf("exit code = %d (err %v), want 7", got, err)
	}
}

func TestProvisionUsage(t *testing.T) {
	for _, args := range [][]string{{"provision"}, {"provision", "web", "db"}} {
		a, r := provisionTree(t, dotfiles)
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: err = %v, want usage", args, err)
		}
		if len(r.calls) != 0 {
			t.Errorf("%v ran ssh", args)
		}
	}
}

func TestExitCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("boom"), 1},
		{exitCodeError(7), 7},
		{fmt.Errorf("provision: %w", exitCodeError(3)), 3},
	} {
		if got := exitCode(tc.err); got != tc.want {
			t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}
