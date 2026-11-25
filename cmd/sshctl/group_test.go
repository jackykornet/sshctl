package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// groupTree is an app whose config has Groups prod (web, db), empty and
// data (old, an Orphaned connection), with prod and data collapsed.
func groupTree(t *testing.T, in string) (*app, *bytes.Buffer) {
	t.Helper()
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "empty"}, {Name: "data"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "site", Group: "prod"}
	cfg.Connections["db"] = config.Connection{Alias: "db", Group: "prod"}
	cfg.Connections["old"] = config.Connection{Alias: "old", Group: "data"}
	cfg.CollapsedGroups = []string{"prod", "data"}
	return cliTree(t, "Host web\nHost db\nHost loose\n", cfg, in)
}

// groupList returns the Group names in a's config, in order.
func groupList(t *testing.T, a *app) []string {
	t.Helper()
	var names []string
	for _, g := range loadCLI(t, a).Groups {
		names = append(names, g.Name)
	}
	return names
}

func TestGroupListShowsNumberNameMemberCountAndDotfiles(t *testing.T) {
	a, out := groupTree(t, "")
	if err := a.catalog().SetGroupDotfiles("", "prod", "https://example.com/dots.git"); err != nil {
		t.Fatal(err)
	}
	if err := a.catalog().SetGroupDotfiles("", "data", config.DotfilesNone); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"group", "list"}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"1  prod                 2  https://example.com/dots.git\n" +
		"2  empty                0  unset\n" +
		"3  data                 1  none (Provision refused)\n"
	if out.String() != want {
		t.Errorf("group list:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestGroupAddCreatesAnEmptyGroupLast(t *testing.T) {
	a, _ := groupTree(t, "")
	if err := a.execute([]string{"group", "add", "web tier"}); err != nil {
		t.Fatal(err)
	}
	if got, want := groupList(t, a), []string{"prod", "empty", "data", "web tier"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	for _, name := range []string{"prod", "7"} {
		if err := a.execute([]string{"group", "add", name}); err == nil {
			t.Errorf("group add %s succeeded, want it refused", name)
		}
	}
}

func TestGroupRenameKeepsMembersPositionAndCollapsed(t *testing.T) {
	a, _ := groupTree(t, "")
	if err := a.execute([]string{"group", "rename", "prod", "live"}); err != nil {
		t.Fatal(err)
	}
	cfg := loadCLI(t, a)
	if got, want := groupList(t, a), []string{"live", "empty", "data"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	if web, db := cfg.Connections["web"], cfg.Connections["db"]; web.Group != "live" || db.Group != "live" || web.Description != "site" {
		t.Errorf("web, db = %+v, %+v, want both in live", web, db)
	}
	if want := []string{"live", "data"}; !slices.Equal(cfg.CollapsedGroups, want) {
		t.Errorf("collapsed = %v, want %v", cfg.CollapsedGroups, want)
	}
	for _, args := range [][]string{{"live", "data"}, {"live", "12"}, {"nowhere", "x"}, {"live"}} {
		if err := a.execute(append([]string{"group", "rename"}, args...)); err == nil {
			t.Errorf("group rename %v succeeded, want it refused", args)
		}
	}
}

func TestGroupMoveSetsThePosition(t *testing.T) {
	a, _ := groupTree(t, "")
	if err := a.execute([]string{"group", "move", "data", "1"}); err != nil {
		t.Fatal(err)
	}
	if got, want := groupList(t, a), []string{"data", "prod", "empty"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	for _, args := range [][]string{{"data", "4"}, {"data", "0"}, {"data", "first"}, {"nowhere", "1"}} {
		if err := a.execute(append([]string{"group", "move"}, args...)); err == nil {
			t.Errorf("group move %v succeeded, want it refused", args)
		}
	}
}

func TestGroupDeleteAsksThenLeavesMembersUngrouped(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		in      string
		deleted bool
		asked   bool
	}{
		{"confirmed", []string{"prod"}, "y\n", true, true},
		{"declined", []string{"prod"}, "n\n", false, true},
		{"no answer", []string{"prod"}, "", false, true},
		{"--yes", []string{"prod", "--yes"}, "", true, false},
		{"--yes first", []string{"--yes", "prod"}, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, out := groupTree(t, tc.in)
			if err := a.execute(append([]string{"group", "delete"}, tc.args...)); err != nil {
				t.Fatal(err)
			}
			const question = "Delete group prod? 2 Connections become ungrouped. [y/N] "
			if got := strings.Contains(out.String(), question); got != tc.asked {
				t.Errorf("asked %v, want %v; output %q", got, tc.asked, out.String())
			}
			cfg := loadCLI(t, a)
			want := []string{"prod", "empty", "data"}
			if tc.deleted {
				want = []string{"empty", "data"}
			}
			if got := groupList(t, a); !slices.Equal(got, want) {
				t.Errorf("groups = %v, want %v", got, want)
			}
			if !tc.deleted {
				return
			}
			if web := cfg.Connections["web"]; web.Group != "" || web.Description != "site" {
				t.Errorf("web = %+v, want it ungrouped keeping its description", web)
			}
			if !slices.Equal(cfg.CollapsedGroups, []string{"data"}) {
				t.Errorf("collapsed = %v, want [data]", cfg.CollapsedGroups)
			}
			snap, err := a.catalog().Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.Entries) != 4 {
				t.Errorf("entries = %+v, want web, db, loose and old kept", snap.Entries)
			}
		})
	}
}

func TestGroupDeleteRefusesAnUnknownGroup(t *testing.T) {
	a, out := groupTree(t, "y\n")
	if err := a.execute([]string{"group", "delete", "nowhere"}); err == nil {
		t.Fatal("deleting an unknown group succeeded")
	}
	if strings.Contains(out.String(), "Delete group") {
		t.Errorf("asked about an unknown group: %q", out.String())
	}
}

// groupsOf returns the Group names of the Space ref ("" for the default
// Space) in a's config, in order.
func groupsOf(t *testing.T, a *app, ref string) []string {
	t.Helper()
	var names []string
	for _, g := range loadCLI(t, a).Groups {
		if g.Space == ref {
			names = append(names, g.Name)
		}
	}
	return names
}

func TestGroupNameInOneSpaceNeedsNoSpaceOption(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"group", "rename", "data", "stats"}); err != nil {
		t.Fatal(err)
	}
	if got, want := groupsOf(t, a, "acme"), []string{"prod", "stats"}; !slices.Equal(got, want) {
		t.Errorf("acme's groups = %v, want %v", got, want)
	}
}

func TestGroupNameInSeveralSpacesNeedsTheSpaceOption(t *testing.T) {
	a, _ := spaceTree(t, "")
	err := a.execute([]string{"group", "rename", "prod", "live"})
	if err == nil || !strings.Contains(err.Error(), "home, acme") || !strings.Contains(err.Error(), "--space") {
		t.Errorf("err = %v, want it to name home, acme and --space", err)
	}
	if err := a.execute([]string{"group", "rename", "--space", "acme", "prod", "live"}); err != nil {
		t.Fatal(err)
	}
	if got, want := groupsOf(t, a, "acme"), []string{"live", "data"}; !slices.Equal(got, want) {
		t.Errorf("acme's groups = %v, want %v", got, want)
	}
	if got, want := groupsOf(t, a, ""), []string{"prod"}; !slices.Equal(got, want) {
		t.Errorf("home's groups = %v, want %v", got, want)
	}
	for _, args := range [][]string{
		{"group", "rename", "--space", "home", "data", "x"},
		{"group", "rename", "nowhere", "x"},
		{"group", "rename", "--space", "nowhere", "prod", "x"},
	} {
		if err := a.execute(args); err == nil {
			t.Errorf("%v succeeded, want it refused", args)
		}
	}
}

func TestGroupNumberNeedsTheSpaceOptionOnlyWithSeveralSpaces(t *testing.T) {
	one, _ := groupTree(t, "")
	if err := one.execute([]string{"group", "move", "3", "1"}); err != nil {
		t.Fatal(err)
	}
	if got, want := groupList(t, one), []string{"data", "prod", "empty"}; !slices.Equal(got, want) {
		t.Errorf("one Space: groups = %v, want %v", got, want)
	}

	a, _ := spaceTree(t, "")
	err := a.execute([]string{"group", "move", "2", "1"})
	if err == nil || !strings.Contains(err.Error(), "--space") {
		t.Errorf("several Spaces: err = %v, want it to ask for --space", err)
	}
	if err := a.execute([]string{"group", "move", "--space", "acme", "2", "1"}); err != nil {
		t.Fatal(err)
	}
	if got, want := groupsOf(t, a, "acme"), []string{"data", "prod"}; !slices.Equal(got, want) {
		t.Errorf("acme's groups = %v, want %v", got, want)
	}
	for _, n := range []string{"0", "3"} {
		if err := a.execute([]string{"group", "rename", "--space", "acme", n, "x"}); err == nil {
			t.Errorf("group number %s succeeded, want it refused", n)
		}
	}
}

func TestGroupAddGoesToTheSpaceOptionOrTheDefaultSpace(t *testing.T) {
	a, _ := spaceTree(t, "")
	for _, args := range [][]string{{"--space", "acme", "stats"}, {"web"}, {"prod", "--space", "empty"}} {
		if err := a.execute(append([]string{"group", "add"}, args...)); err != nil {
			t.Fatalf("group add %v: %v", args, err)
		}
	}
	for ref, want := range map[string][]string{"": {"prod", "web"}, "acme": {"prod", "data", "stats"}, "empty": {"prod"}} {
		if got := groupsOf(t, a, ref); !slices.Equal(got, want) {
			t.Errorf("space %q: groups = %v, want %v", ref, got, want)
		}
	}
	if err := a.execute([]string{"group", "add", "--space", "nowhere", "x"}); err == nil {
		t.Error("group add in an unknown space succeeded")
	}
}

func TestGroupListShowsEverySpaceOrTheSpaceOption(t *testing.T) {
	a, out := spaceTree(t, "")
	if err := a.execute([]string{"group", "list"}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"home                 1  prod                 1  unset\n" +
		"acme                 1  prod                 1  unset\n" +
		"acme                 2  data                 0  unset\n"
	if out.String() != want {
		t.Errorf("group list:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Reset()
	if err := a.execute([]string{"group", "list", "--space", "acme"}); err != nil {
		t.Fatal(err)
	}
	want = "" +
		"1  prod                 1  unset\n" +
		"2  data                 0  unset\n"
	if out.String() != want {
		t.Errorf("group list --space acme:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestGroupDeleteCountsMembersInItsOwnSpace(t *testing.T) {
	a, out := spaceTree(t, "y\n")
	if err := a.execute([]string{"group", "delete", "--space", "acme", "prod"}); err != nil {
		t.Fatal(err)
	}
	if want := "Delete group prod? 1 Connection becomes ungrouped. [y/N] "; out.String() != want {
		t.Errorf("asked %q, want %q", out.String(), want)
	}
	if got, want := groupsOf(t, a, ""), []string{"prod"}; !slices.Equal(got, want) {
		t.Errorf("home's groups = %v, want %v", got, want)
	}
	if db := loadCLI(t, a).Connections["db"]; db.Space != "acme" || db.Group != "" {
		t.Errorf("db = %+v, want it ungrouped in acme", db)
	}
}

// loadCLI reads a's sshctl config back from disk.
func loadCLI(t *testing.T, a *app) *config.Config {
	t.Helper()
	c, err := config.Load(a.cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// recordTerminals makes a's group connect record the aliases it would open
// a terminal for, instead of opening one.
func recordTerminals(a *app) *[]string {
	var opened []string
	a.openTerminal = func(alias, key string) error {
		opened = append(opened, alias)
		return nil
	}
	return &opened
}

func TestGroupConnectTakesZeroForTheUngroupedConnections(t *testing.T) {
	a, _ := groupTree(t, "")
	opened := recordTerminals(a)
	if err := a.execute([]string{"group", "connect", "0"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"loose"}; !slices.Equal(*opened, want) {
		t.Errorf("group connect 0 opened %v, want %v", *opened, want)
	}

	s, _ := spaceTree(t, "")
	opened = recordTerminals(s)
	if err := s.execute([]string{"group", "connect", "--space", "home", "0"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"other", "solo"}; !slices.Equal(*opened, want) {
		t.Errorf("group connect --space home 0 opened %v, want %v", *opened, want)
	}
	if err := s.execute([]string{"group", "connect", "0"}); err == nil || !strings.Contains(err.Error(), "--space") {
		t.Errorf("group connect 0 with several Spaces: err = %v, want it to ask for --space", err)
	}
}

func TestGroupCommandsThatNeedAGroupRefuseZero(t *testing.T) {
	for _, args := range [][]string{{"rename", "0", "x"}, {"move", "0", "1"}, {"delete", "0", "--yes"}} {
		a, _ := groupTree(t, "")
		err := a.execute(append([]string{"group"}, args...))
		if err == nil || !strings.Contains(err.Error(), "ungrouped") {
			t.Errorf("group %v: err = %v, want it refused, saying ungrouped is not a Group", args, err)
		}
		if got, want := groupList(t, a), []string{"prod", "empty", "data"}; !slices.Equal(got, want) {
			t.Errorf("group %v: groups = %v, want %v", args, got, want)
		}
	}
}
