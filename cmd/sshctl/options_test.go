package main

import (
	"reflect"
	"strings"
	"testing"
)

// Options follow the GNU convention (ADR-0011): --long, the short forms -c
// -s -g -y -h, --opt value and --opt=value, anywhere after the command name.
// Every spelling of a call does what the first one does: same output, same
// error and same config afterwards.
func TestOptionsParseTheSameInEveryPositionAndForm(t *testing.T) {
	for _, calls := range [][][]string{
		{
			{"list", "--space", "acme"},
			{"list", "--space=acme"},
			{"list", "-s", "acme"},
		},
		{
			{"group", "--space", "acme", "list"},
			{"group", "list", "--space", "acme"},
			{"group", "list", "--space=acme"},
			{"group", "-s", "acme", "list"},
			{"group", "--space=acme", "list"},
			{"group", "list", "-s", "2"},
		},
		{
			{"group", "--space", "acme", "delete", "data", "--yes"},
			{"group", "delete", "--yes", "data", "--space", "acme"},
			{"group", "-y", "delete", "-s", "acme", "data"},
			{"group", "delete", "data", "-y", "--space=acme"},
		},
		{
			{"group", "--space", "acme", "rename", "prod", "live"},
			{"group", "rename", "prod", "--space", "acme", "live"},
			{"group", "rename", "prod", "live", "-s", "acme"},
		},
		{
			{"remove", "web", "--yes"},
			{"remove", "--yes", "web"},
			{"remove", "-y", "web"},
			{"remove", "web", "-y"},
		},
		{
			{"key", "use", "--space", "acme", "none"},
			{"key", "use", "none", "--space=acme"},
			{"key", "--space", "acme", "use", "none"},
			{"key", "-s", "acme", "use", "none"},
		},
		{
			{"edit", "solo", "--description", "lone", "--group", "prod"},
			{"edit", "--description=lone", "solo", "-g", "prod"},
			{"edit", "--group=prod", "--description", "lone", "solo"},
		},
		{
			{"provision", "default", "https://example.com/d.git", "--group", "prod"},
			{"provision", "default", "--group=prod", "https://example.com/d.git"},
			{"provision", "default", "-g", "prod", "https://example.com/d.git"},
			{"provision", "--group", "prod", "default", "https://example.com/d.git"},
		},
		{
			{"space", "dotfiles", "acme", "--unset"},
			{"space", "dotfiles", "--unset", "acme"},
			{"space", "--unset", "dotfiles", "acme"},
		},
		{
			{"tunnel", "add", "web", "8080"},
			{"tunnel", "add", "--", "web", "8080"},
		},
	} {
		want := spell(t, calls[0])
		if want.err != "" {
			t.Errorf("sshctl %s: %s", strings.Join(calls[0], " "), want.err)
		}
		for _, call := range calls[1:] {
			if got := spell(t, call); !reflect.DeepEqual(got, want) {
				t.Errorf("sshctl %s:\n%+v\nwant, as sshctl %s:\n%+v", strings.Join(call, " "), got, strings.Join(calls[0], " "), want)
			}
		}
	}
}

// spelled is what a call did: its output, its error and the config after.
type spelled struct {
	out, err string
	config   any
}

// spell runs args on a fresh spaceTree and returns what it did.
func spell(t *testing.T, args []string) spelled {
	t.Helper()
	a, out := spaceTree(t, "")
	var s spelled
	if err := a.execute(args); err != nil {
		s.err = err.Error()
	}
	s.out = out.String()
	s.config = *loadCLI(t, a)
	return s
}

// Options outside the GNU convention, unknown ones and ones a command does
// not take are refused with the command's usage, and so are a missing value,
// a value for a flag and an option given twice.
func TestOptionsOutsideTheConventionAreRefused(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list", "-space", "acme"}, "unknown option -space"},
		{[]string{"group", "list", "-space=acme"}, "unknown option -space=acme"},
		{[]string{"list", "--bogus"}, "unknown option --bogus"},
		{[]string{"list", "-x"}, "unknown option -x"},
		{[]string{"list", "--yes"}, "sshctl list does not take --yes"},
		{[]string{"list", "--space"}, "--space needs a value"},
		{[]string{"remove", "web", "--yes=1"}, "--yes takes no value"},
		{[]string{"list", "-s", "acme", "--space", "home"}, "--space given twice"},
		{[]string{"list", "/space", "acme"}, ""},
		{[]string{"provision", "default", "--json"}, "unknown option --json"},
	} {
		a, _ := spaceTree(t, "")
		err := a.execute(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "usage: sshctl "+tc.args[0]) {
			t.Errorf("sshctl %s: err = %v, want %q and the usage", strings.Join(tc.args, " "), err, tc.want)
		}
	}
}

// -h and --help anywhere after a command print its usage.
func TestHelpOptionAnywhere(t *testing.T) {
	for _, args := range [][]string{
		{"group", "--help"}, {"group", "-h"}, {"group", "list", "--help"}, {"group", "--space", "acme", "list", "-h"},
	} {
		a, out := spaceTree(t, "")
		if err := a.execute(args); err != nil {
			t.Fatal(err)
		}
		if want := "Usage: sshctl " + usage("group") + "\n"; out.String() != want {
			t.Errorf("sshctl %s = %q, want the group usage", strings.Join(args, " "), out)
		}
	}
}
