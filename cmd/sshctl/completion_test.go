package main

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
)

// completeTree is an app with the Connections web01, web02 and an alias
// named like the list command, the Spaces default and acme, and the Groups
// prod (default Space) and edge (acme).
func completeTree(t *testing.T) *app {
	t.Helper()
	a, _ := cliTree(t, "Host web01\nHost web02\nHost list\n", config.New(), "")
	c := a.catalog()
	if err := c.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddGroup("", "prod"); err != nil {
		t.Fatal(err)
	}
	ref, _ := mustSnapshot(t, a).SpaceRef("acme")
	if err := c.AddGroup(ref, "edge"); err != nil {
		t.Fatal(err)
	}
	return a
}

// complete runs the hidden completion command for words, the words after
// sshctl with the one being completed last, and returns its candidates.
func complete(t *testing.T, a *app, words ...string) []string {
	t.Helper()
	out := a.out.(interface {
		String() string
		Reset()
	})
	out.Reset()
	args := append([]string{completeCommand, strconv.Itoa(len(words) - 1)}, words...)
	if err := a.execute(args); err != nil {
		t.Fatalf("sshctl %s: %v", strings.Join(args, " "), err)
	}
	return strings.Fields(out.String())
}

func TestCompleteTheFirstWordWithCommandsAndAliases(t *testing.T) {
	a := completeTree(t)
	got := complete(t, a, "")
	for _, want := range []string{"list", "space", "group", "completion", "web01", "web02"} {
		if !slices.Contains(got, want) {
			t.Errorf("first word: %v lacks %q", got, want)
		}
	}
	if slices.Contains(got, completeCommand) {
		t.Errorf("first word offers the hidden %s: %v", completeCommand, got)
	}
	if got := complete(t, a, "we"); !slices.Equal(got, []string{"web01", "web02"}) {
		t.Errorf("first word we = %v, want web01 web02", got)
	}
}

func mustSnapshot(t *testing.T, a *app) catalog.Snapshot {
	t.Helper()
	snap, err := a.catalog().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// Each position the completion scripts ask about gets what may stand
// there.
func TestCompleteEachPosition(t *testing.T) {
	a := completeTree(t)
	aliases := []string{"web01", "web02", "list"}
	for _, tc := range []struct {
		words []string
		want  []string
	}{
		{[]string{"-c", ""}, aliases},
		{[]string{"--connect", "w"}, []string{"web01", "web02"}},
		{[]string{"--connect=w"}, []string{"--connect=web01", "--connect=web02"}},
		{[]string{"-c", "web01", ""}, nil},
		{[]string{"web01", ""}, nil},
		{[]string{"web01", "--", ""}, nil},
		{[]string{"add", ""}, aliases},
		{[]string{"edit", "w"}, []string{"web01", "web02"}},
		{[]string{"remove", ""}, aliases},
		{[]string{"remove", "web01", ""}, nil},
		{[]string{"info", ""}, aliases},
		{[]string{"info", "web01", ""}, nil},
		{[]string{"info", "web01", "--"}, []string{"--json", "--help"}},
		{[]string{"list", "--j"}, []string{"--json"}},
		{[]string{"bootstrap", ""}, aliases},
		{[]string{"provision", ""}, append([]string{"default"}, aliases...)},
		{[]string{"tunnel", "w"}, []string{"web01", "web02"}},
		{[]string{"tunnel", "rm", ""}, aliases},
		{[]string{"space", ""}, []string{"list", "add", "rename", "delete", "move", "dotfiles"}},
		{[]string{"space", "rename", ""}, []string{"default", "acme"}},
		{[]string{"space", "rename", "acme", ""}, nil},
		{[]string{"group", "-s", ""}, []string{"default", "acme"}},
		{[]string{"list", "--space=a"}, []string{"--space=acme"}},
		{[]string{"group", "rename", ""}, []string{"prod", "edge"}},
		{[]string{"group", "--space", "acme", "rename", ""}, []string{"edge"}},
		{[]string{"group", "rename", "--space=default", ""}, []string{"prod"}},
		{[]string{"add", "web03", "--group", ""}, []string{"prod", "edge"}},
		{[]string{"add", "web03", "-s", "acme", "-g", ""}, []string{"edge"}},
		{[]string{"provision", "default", "--group", ""}, []string{"prod", "edge"}},
		{[]string{"edit", "web01", "--port", ""}, nil},
		{[]string{"experimental", ""}, []string{"list", "enable", "disable"}},
		{[]string{"experimental", "enable", ""}, []string{"bootstrap", "provision"}},
		{[]string{"completion", ""}, []string{"bash", "zsh", "fish", "powershell"}},
		{[]string{"remove", "web01", "--"}, []string{"--yes", "--help"}},
		{[]string{"key", "use", "--s"}, []string{"--space"}},
		{[]string{"group", "--"}, []string{"--space", "--yes", "--json", "--help"}},
		{[]string{"--"}, []string{"--connect", "--help", "--version"}},
		{[]string{"history", "--v"}, nil},
	} {
		got := complete(t, a, tc.words...)
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("sshctl %s<TAB> = %v, want %v", strings.Join(tc.words, " "), got, want)
		}
	}
}

// A form of any command whose first argument is an alias completes
// aliases there, so a new such command needs nothing more.
func TestCompleteAliasesWhereEveryFormTakesOne(t *testing.T) {
	a := completeTree(t)
	for _, c := range commands {
		for _, f := range c.forms {
			if p := positionals(f.args); len(p) == 0 || p[0] != "<alias>" {
				continue
			}
			words := []string{c.name}
			if f.sub != "" {
				words = append(words, f.sub)
			}
			got := complete(t, a, append(words, "web")...)
			if !slices.Equal(got, []string{"web01", "web02"}) {
				t.Errorf("sshctl %s web<TAB> = %v, want web01 web02", strings.Join(words, " "), got)
			}
		}
	}
}

// A shell that drops an empty last argument still completes a new word,
// and nonsense prints nothing rather than an error.
func TestCompleteAnEmptyWordTheShellDropped(t *testing.T) {
	a := completeTree(t)
	if err := a.execute([]string{completeCommand, "1", "bootstrap"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(output(a)); !slices.Equal(got, []string{"web01", "web02", "list"}) {
		t.Errorf("bootstrap <TAB> with the empty word dropped = %v", got)
	}
	for _, args := range [][]string{{}, {"x"}, {"-1"}, {"5", "list"}} {
		a := completeTree(t)
		if err := a.execute(append([]string{completeCommand}, args...)); err != nil || output(a) != "" {
			t.Errorf("sshctl %s %v = %q, %v; want nothing", completeCommand, args, output(a), err)
		}
	}
}

// sshctl completion prints each shell's script, which runs the hidden
// command, and refuses another shell with its usage.
func TestCompletionPrintsEachShellsScript(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		a := completeTree(t)
		if err := a.execute([]string{"completion", shell}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output(a), completeCommand) {
			t.Errorf("completion %s does not run %s:\n%s", shell, completeCommand, output(a))
		}
	}
	a := completeTree(t)
	if err := a.execute([]string{"completion", "tcsh"}); err == nil || !strings.HasPrefix(err.Error(), "usage: sshctl completion") {
		t.Errorf("completion tcsh: err = %v, want usage", err)
	}
}
