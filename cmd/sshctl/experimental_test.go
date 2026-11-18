package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// featuresOff is an app over a config with every Experimental feature off,
// whose ssh fails the test if anything runs it.
func featuresOff(t *testing.T) (*app, *bytes.Buffer) {
	t.Helper()
	a, out := cliTree(t, "Host web\n", config.New(), "")
	for _, f := range config.Features {
		if err := a.catalog().SetFeature(f, false); err != nil {
			t.Fatal(err)
		}
	}
	a.runSSH = func(_ context.Context, args []string, _ io.Writer) error {
		t.Errorf("ran ssh %v while its feature is off", args)
		return nil
	}
	return a, out
}

func TestExperimentalListEnableAndDisable(t *testing.T) {
	a, out := featuresOff(t)
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"experimental", "list"}, "bootstrap  off\nprovision  off\n"},
		{[]string{"experimental", "enable", "provision"}, ""},
		{[]string{"experimental", "list"}, "bootstrap  off\nprovision  on\n"},
		{[]string{"experimental", "disable", "provision"}, ""},
		{[]string{"experimental", "list"}, "bootstrap  off\nprovision  off\n"},
	}
	for _, s := range steps {
		out.Reset()
		if err := a.execute(s.args); err != nil {
			t.Fatalf("%v: %v", s.args, err)
		}
		if out.String() != s.want {
			t.Errorf("%v printed %q, want %q", s.args, out.String(), s.want)
		}
	}
}

func TestExperimentalRefusesBadArguments(t *testing.T) {
	a, _ := featuresOff(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"experimental"}, "usage: sshctl " + experimentalUsage},
		{[]string{"experimental", "enable"}, "usage: sshctl " + experimentalUsage},
		{[]string{"experimental", "on", "bootstrap"}, "usage: sshctl " + experimentalUsage},
		{[]string{"experimental", "enable", "sync"}, `unknown experimental feature "sync"; the experimental features are bootstrap, provision`},
	} {
		if err := a.execute(tt.args); err == nil || err.Error() != tt.want {
			t.Errorf("%v = %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestCommandsRefuseWhileTheirFeatureIsOff(t *testing.T) {
	const (
		bootstrapOff = "Bootstrap is experimental and off. Turn it on with: sshctl experimental enable bootstrap"
		provisionOff = "Provision is experimental and off. Turn it on with: sshctl experimental enable provision"
	)
	a, _ := featuresOff(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"bootstrap", "web"}, bootstrapOff},
		{[]string{"bootstrap", "--", "web"}, bootstrapOff},
		{[]string{"provision", "web"}, provisionOff},
		{[]string{"provision", "default"}, provisionOff},
		{[]string{"provision", "default", dotfiles}, provisionOff},
		{[]string{"space", "dotfiles", "default", dotfiles}, provisionOff},
	} {
		if err := a.execute(tt.args); err == nil || err.Error() != tt.want {
			t.Errorf("%v = %v, want %q", tt.args, err, tt.want)
		}
	}
	c := load(t, a)
	if c.Provision.Dotfiles != "" || c.Spaces[0].Dotfiles != "" {
		t.Errorf("a refused command saved dotfiles: global %q, space %q", c.Provision.Dotfiles, c.Spaces[0].Dotfiles)
	}
}

func TestCommandUsageStillShowsWhileItsFeatureIsOff(t *testing.T) {
	a, out := featuresOff(t)
	if err := a.execute([]string{"bootstrap", "--help"}); err != nil {
		t.Fatal(err)
	}
	if want := "Usage: sshctl " + bootstrapUsage + "\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

func TestHelpMarksExperimentalFeaturesThatAreOff(t *testing.T) {
	var off, on bytes.Buffer
	help(&off, config.Experimental{})
	help(&on, config.Experimental{Bootstrap: true, Provision: true})
	for _, line := range []string{
		"Install your key on the host and use it (experimental, off)",
		"Apply your dotfiles to the host with chezmoi (experimental, off)",
		"Show or set the global or a Group's dotfiles repo (experimental, off)",
		"Show or set a Space's dotfiles repo (experimental, off)",
	} {
		if !strings.Contains(off.String(), line+"\n") {
			t.Errorf("help with features off lacks %q", line)
		}
	}
	if strings.Contains(on.String(), "(experimental, off)") {
		t.Errorf("help with every feature on marks one off:\n%s", on.String())
	}
	if !strings.Contains(on.String(), "experimental enable|disable <feature>") {
		t.Errorf("help lacks the experimental command:\n%s", on.String())
	}
}
