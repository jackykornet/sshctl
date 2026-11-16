package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// spaceTree is an app whose config has the default Space (renamed home)
// with Group prod (web) and the loose Connection solo, and Space acme with
// Groups prod (db) and data (empty) and Space empty with nothing in it.
func spaceTree(t *testing.T, in string) (*app, *bytes.Buffer) {
	t.Helper()
	cfg := config.New()
	cfg.Spaces = []config.Space{{Name: "home", Default: true}, {Name: "acme"}, {Name: "empty"}}
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "prod", Space: "acme"}, {Name: "data", Space: "acme"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
	cfg.Connections["solo"] = config.Connection{Alias: "solo", Description: "loose"}
	cfg.Connections["db"] = config.Connection{Alias: "db", Space: "acme", Group: "prod"}
	return cliTree(t, "Host web\nHost db\nHost solo\nHost other\n", cfg, in)
}

// spaceList returns the Space names in a's config, in order.
func spaceList(t *testing.T, a *app) []string {
	t.Helper()
	var names []string
	for _, s := range loadCLI(t, a).Spaces {
		names = append(names, s.Name)
	}
	return names
}

func TestSpaceListShowsNumberNameGroupsAndConnections(t *testing.T) {
	a, out := spaceTree(t, "")
	if err := a.execute([]string{"space", "list"}); err != nil {
		t.Fatal(err)
	}
	// other has no metadata, so it is in the default Space too.
	want := "" +
		"1  home                 1  3  (default)\n" +
		"2  acme                 2  1\n" +
		"3  empty                0  0\n"
	if out.String() != want {
		t.Errorf("space list:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestSpaceAddCreatesAnEmptySpaceLast(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"space", "add", "prive"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceList(t, a), []string{"home", "acme", "empty", "prive"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for _, name := range []string{"acme", "7", "a/b", ""} {
		if err := a.execute([]string{"space", "add", name}); err == nil {
			t.Errorf("space add %q succeeded, want it refused", name)
		}
	}
}

func TestSpaceRenameKeepsContentsAndTheDefault(t *testing.T) {
	a, _ := spaceTree(t, "")
	for _, args := range [][]string{{"home", "house"}, {"acme", "acme-corp"}} {
		if err := a.execute(append([]string{"space", "rename"}, args...)); err != nil {
			t.Fatal(err)
		}
	}
	cfg := loadCLI(t, a)
	if got, want := spaceList(t, a), []string{"house", "acme-corp", "empty"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	if !cfg.Spaces[0].Default {
		t.Errorf("house = %+v, want it still the default Space", cfg.Spaces[0])
	}
	if db := cfg.Connections["db"]; db.Space != "acme-corp" || db.Group != "prod" {
		t.Errorf("db = %+v, want it in acme-corp's prod", db)
	}
	for _, args := range [][]string{{"house", "empty"}, {"house", "12"}, {"nowhere", "x"}, {"house"}} {
		if err := a.execute(append([]string{"space", "rename"}, args...)); err == nil {
			t.Errorf("space rename %v succeeded, want it refused", args)
		}
	}
}

func TestSpaceDeleteRefusesTheDefaultAndANonEmptySpace(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"space", "delete", "empty"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceList(t, a), []string{"home", "acme"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	err := a.execute([]string{"space", "delete", "acme"})
	if err == nil || !strings.Contains(err.Error(), "groups data, prod") || !strings.Contains(err.Error(), "connections db") {
		t.Errorf("deleting acme: err = %v, want it refused naming its groups and connections", err)
	}
	for _, args := range [][]string{{"home"}, {"nowhere"}, {}} {
		if err := a.execute(append([]string{"space", "delete"}, args...)); err == nil {
			t.Errorf("space delete %v succeeded, want it refused", args)
		}
	}
	if got, want := spaceList(t, a), []string{"home", "acme"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
}

func TestSpaceMoveSetsThePosition(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"space", "move", "empty", "1"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceList(t, a), []string{"empty", "home", "acme"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for _, args := range [][]string{{"acme", "4"}, {"acme", "0"}, {"acme", "first"}, {"nowhere", "1"}} {
		if err := a.execute(append([]string{"space", "move"}, args...)); err == nil {
			t.Errorf("space move %v succeeded, want it refused", args)
		}
	}
}

func TestListWithSpaceShowsOnlyThatSpacesConnections(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--space", "acme"}, "db                       [prod]\n"},
		{[]string{"--space", "home"}, "web                      [prod]\nsolo                     loose\nother\n"},
		{[]string{"--space", "empty"}, ""},
	} {
		a, out := spaceTree(t, "")
		if err := a.execute(append([]string{"list"}, tc.args...)); err != nil {
			t.Fatal(err)
		}
		if out.String() != tc.want {
			t.Errorf("list %v:\n%s\nwant:\n%s", tc.args, out.String(), tc.want)
		}
	}
	for _, args := range [][]string{{"--space", "nowhere"}, {"--space"}, {"acme"}} {
		a, _ := spaceTree(t, "")
		if err := a.execute(append([]string{"list"}, args...)); err == nil {
			t.Errorf("list %v succeeded, want it refused", args)
		}
	}
}
