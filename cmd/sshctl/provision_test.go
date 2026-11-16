package main

import (
	"bytes"
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

	"github.com/sshctl/sshctl/internal/catalog"
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
		{"provision", "default", "--group", "prod"},
		{"provision", "default", dotfiles, "--group"},
		{"provision", "default", dotfiles, "--group", "a", "--group", "b"},
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
	cfg.SelectedIdentity = "/k/id"
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

func TestProvisionDefaultSetsListsAndUnsetsAGroupsRepo(t *testing.T) {
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "lab"}}
	a, out := cliTree(t, "Host web\n", cfg, "")
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"provision", "default"}, "global: unset\ngroup prod: unset\ngroup lab: unset\n"},
		{[]string{"provision", "default", dotfiles, "--group", "prod"}, ""},
		{[]string{"provision", "default", "--group", "lab", "none"}, ""},
		{[]string{"provision", "default"}, "global: unset\ngroup prod: " + dotfiles + "\ngroup lab: none (Provision refused)\n"},
		{[]string{"provision", "default", "--unset", "--group", "prod"}, ""},
		{[]string{"provision", "default"}, "global: unset\ngroup prod: unset\ngroup lab: none (Provision refused)\n"},
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

func TestProvisionDefaultRefusesAnUnknownGroup(t *testing.T) {
	a, _ := cliTree(t, "Host web\n", config.New(), "")
	if err := a.execute([]string{"provision", "default", dotfiles, "--group", "nowhere"}); err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("err = %v, want one naming the group", err)
	}
}

func TestProvisionUsesTheGroupsRepoOrRefusesForItsGroup(t *testing.T) {
	const prodRepo = "https://example.com/prod.git"
	for _, tc := range []struct {
		name, group string
		want        string
		err         string
	}{
		{"group repo", prodRepo, prodRepo, ""},
		{"global fallback", "", dotfiles, ""},
		{"group none", config.DotfilesNone, "", "group prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.New()
			cfg.Provision.Dotfiles = dotfiles
			cfg.Groups = []config.Group{{Name: "prod", Dotfiles: tc.group}}
			cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
			a, out := cliTree(t, "Host web\n", cfg, "")
			r := &sshRecorder{}
			a.runSSH = r.run
			err := a.execute([]string{"provision", "web"})
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Errorf("err = %v, want one containing %q", err, tc.err)
				}
				if len(r.calls) != 0 {
					t.Errorf("ssh ran %d times, want none", len(r.calls))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			script := r.calls[len(r.calls)-1]
			if !strings.Contains(out.String(), tc.want) || !strings.Contains(script[len(script)-1], tc.want) {
				t.Errorf("provisioned with %q / %q, want %q", out.String(), r.calls, tc.want)
			}
		})
	}
}

// askTree is a tree where no dotfiles repo applies to web, which is in
// Group group unless that is "", and in is what the user types.
func askTree(t *testing.T, group, in string) (*app, *bytes.Buffer, *sshRecorder) {
	t.Helper()
	cfg := config.New()
	if group != "" {
		cfg.Groups = []config.Group{{Name: group}}
		cfg.Connections["web"] = config.Connection{Alias: "web", Group: group}
	}
	a, out := cliTree(t, "Host web\n", cfg, in)
	r := &sshRecorder{}
	a.runSSH = r.run
	return a, out, r
}

// provisionedWith reports whether the last ssh call's script applies repo.
func provisionedWith(r *sshRecorder, repo string) bool {
	if len(r.calls) == 0 {
		return false
	}
	argv := r.calls[len(r.calls)-1]
	return strings.Contains(argv[len(argv)-1], repo)
}

func TestProvisionAsksForTheRepoAndCanUseItOnce(t *testing.T) {
	const repo = "https://example.com/once.git"
	a, out, r := askTree(t, "prod", repo+"\no\n")
	if err := a.execute([]string{"provision", "web"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Dotfiles repo URL") {
		t.Errorf("output %q does not ask for the repo", out.String())
	}
	if !provisionedWith(r, repo) {
		t.Errorf("ssh calls %q do not apply %s", r.calls, repo)
	}
	c := load(t, a)
	if c.Provision.Dotfiles != "" || c.Groups[0].Dotfiles != "" {
		t.Errorf("once saved global %q, group %q; want nothing", c.Provision.Dotfiles, c.Groups[0].Dotfiles)
	}
}

func TestProvisionSavesTheAskedRepoWhereChosen(t *testing.T) {
	const repo = "https://example.com/asked.git"
	for _, tc := range []struct {
		choice             string
		wantGlobal, wantGr string
	}{
		{"g", repo, ""},
		{"r", "", repo},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			a, _, r := askTree(t, "prod", repo+"\n"+tc.choice+"\n")
			if err := a.execute([]string{"provision", "web"}); err != nil {
				t.Fatal(err)
			}
			if !provisionedWith(r, repo) {
				t.Errorf("ssh calls %q do not apply %s", r.calls, repo)
			}
			c := load(t, a)
			if c.Provision.Dotfiles != tc.wantGlobal || c.Groups[0].Dotfiles != tc.wantGr {
				t.Errorf("saved global %q, group %q; want %q, %q", c.Provision.Dotfiles, c.Groups[0].Dotfiles, tc.wantGlobal, tc.wantGr)
			}
		})
	}
}

func TestProvisionOffersTheGroupOnlyForAGroupedConnection(t *testing.T) {
	a, out, _ := askTree(t, "prod", dotfiles+"\no\n")
	if err := a.execute([]string{"provision", "web"}); err != nil {
		t.Fatal(err)
	}
	if want := "Save for: (g)lobal, (r) group prod, (o)nce: "; !strings.Contains(out.String(), want) {
		t.Errorf("output %q does not offer %q", out.String(), want)
	}

	a, out, r := askTree(t, "", dotfiles+"\nr\no\n")
	if err := a.execute([]string{"provision", "web"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "group") {
		t.Errorf("output %q offers a group for an ungrouped Connection", out.String())
	}
	if strings.Count(out.String(), "Save for: (g)lobal, (o)nce: ") != 2 {
		t.Errorf("output %q: want r refused and the choice asked again", out.String())
	}
	if !provisionedWith(r, dotfiles) || load(t, a).Provision.Dotfiles != "" {
		t.Error("want the repo used once and not saved")
	}
}

func TestProvisionStopsWhenTheAskIsNotAnswered(t *testing.T) {
	for _, in := range []string{"", "\n", dotfiles + "\n", dotfiles + "\nx\n"} {
		a, _, r := askTree(t, "prod", in)
		err := a.execute([]string{"provision", "web"})
		if err == nil {
			t.Errorf("input %q: provisioned, want an error", in)
		}
		if len(r.calls) != 0 {
			t.Errorf("input %q: ssh ran %q", in, r.calls)
		}
		if c := load(t, a); c.Provision.Dotfiles != "" || c.Groups[0].Dotfiles != "" {
			t.Errorf("input %q: saved a repo", in)
		}
	}
}

func TestProvisionNeverAsksWhenRefused(t *testing.T) {
	cfg := config.New()
	cfg.Provision.Dotfiles = dotfiles
	cfg.Groups = []config.Group{{Name: "prod", Dotfiles: config.DotfilesNone}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
	a, out := cliTree(t, "Host web\n", cfg, dotfiles+"\no\n")
	a.runSSH = (&sshRecorder{}).run
	if err := a.execute([]string{"provision", "web"}); err == nil {
		t.Error("provisioned a refused Connection")
	}
	if strings.Contains(out.String(), "URL") {
		t.Errorf("output %q asks for a repo", out.String())
	}
}

// Typing none at the first-time prompt is the refuse setting, never a repo.
func TestProvisionTypedNoneRefusesAndNeverReachesChezmoi(t *testing.T) {
	for _, tc := range []struct {
		choice             string
		wantGlobal, wantGr string
	}{
		{"g", config.DotfilesNone, ""},
		{"r", "", config.DotfilesNone},
		{"o", "", ""},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			a, _, r := askTree(t, "prod", "none\n"+tc.choice+"\n")
			err := a.execute([]string{"provision", "web"})
			if !errors.Is(err, catalog.ErrProvisionRefused) {
				t.Errorf("err = %v, want the refusal", err)
			}
			if len(r.calls) != 0 {
				t.Errorf("ssh ran %q, want nothing", r.calls)
			}
			c := load(t, a)
			if c.Provision.Dotfiles != tc.wantGlobal || c.Groups[0].Dotfiles != tc.wantGr {
				t.Errorf("saved global %q, group %q; want %q, %q", c.Provision.Dotfiles, c.Groups[0].Dotfiles, tc.wantGlobal, tc.wantGr)
			}
		})
	}
}

func TestBootstrapAndProvisionTakeTheAliasAfterDashDash(t *testing.T) {
	a, r := provisionTree(t, dotfiles)
	if err := a.execute([]string{"provision", "--", "web"}); err != nil {
		t.Fatalf("provision -- web: %v", err)
	}
	if len(r.calls) != 2 {
		t.Errorf("ssh ran %q, want the check and the script", r.calls)
	}
	b, out, _ := bootstrapTree(t, "Host web\n  HostName 10.0.0.5\n")
	if err := b.execute([]string{"bootstrap", "--", "web"}); err != nil {
		t.Fatalf("bootstrap -- web: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"bootstrap", "--"}, {"bootstrap", "--", "web", "db"}, {"provision", "-x"}} {
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%q: err = %v, want usage", args, err)
		}
	}
}
