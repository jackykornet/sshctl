package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

func TestAddAndEditRefuseAnUnknownGroupWithAHint(t *testing.T) {
	for _, edit := range []bool{false, true} {
		cfg := config.New()
		cfg.Groups = []config.Group{{Name: "prod"}}
		cfg.Connections["web"] = config.Connection{Alias: "web", Description: "site", Group: "prod"}
		a, _ := cliTree(t, "Host web\n", cfg, "\nnowhere\n")
		err := a.metadata([]string{"web"}, edit)
		if err == nil || !strings.Contains(err.Error(), "sshctl group add nowhere") {
			t.Fatalf("edit=%v: err = %v, want a hint to run sshctl group add nowhere", edit, err)
		}
		if got := loadCLI(t, a).Connections["web"]; got.Group != "prod" {
			t.Errorf("edit=%v: web = %+v, want it left in prod", edit, got)
		}
	}
}

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

// loadCLI reads a's sshctl config back from disk.
func loadCLI(t *testing.T, a *app) *config.Config {
	t.Helper()
	c, err := config.Load(a.cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
