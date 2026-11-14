package catalog

import (
	"errors"
	"slices"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// groupNames lists the Snapshot's Groups by name, in order.
func groupNames(t *testing.T, tr *tree) []string {
	t.Helper()
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range snap.Groups {
		names = append(names, g.Name)
	}
	return names
}

func TestAddGroupAppendsAnEmptyGroup(t *testing.T) {
	tr := newTree(t, true, "")
	for _, name := range []string{"prod", "staging"} {
		if err := tr.cat.AddGroup(name); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := groupNames(t, tr), []string{"prod", "staging"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestAddGroupRefusesTakenAndInvalidNames(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddGroup("prod"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want error
	}{
		{"prod", ErrGroupExists},
		{"12", config.ErrGroupName},
		{"", config.ErrGroupName},
	} {
		if err := tr.cat.AddGroup(tc.name); !errors.Is(err, tc.want) {
			t.Errorf("AddGroup(%q) = %v, want %v", tc.name, err, tc.want)
		}
	}
	if got, want := groupNames(t, tr), []string{"prod"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestRenameGroupMovesMembersAndKeepsPositionAndCollapsed(t *testing.T) {
	tr := newTree(t, true, "")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "staging"}, {Name: "data"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "site", Group: "staging"}
	cfg.Connections["db"] = config.Connection{Alias: "db", Group: "data"}
	cfg.CollapsedGroups = []string{"staging", "data"}
	tr.seed(t, cfg)
	if err := tr.cat.RenameGroup("staging", "stage"); err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(t, tr), []string{"prod", "stage", "data"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	got := tr.config(t)
	if web := got.Connections["web"]; web.Group != "stage" || web.Description != "site" {
		t.Errorf("web = %+v, want it moved to stage with its description", web)
	}
	if db := got.Connections["db"]; db.Group != "data" {
		t.Errorf("db = %+v, want it left in data", db)
	}
	if want := []string{"stage", "data"}; !slices.Equal(got.CollapsedGroups, want) {
		t.Errorf("collapsed = %v, want %v", got.CollapsedGroups, want)
	}
}

func TestDeleteGroupLeavesMembersUngroupedAndKeepsHostBlocks(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddGroup("prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddGroup("data"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("", Spec{Alias: "api", Description: "API", Group: "prod", Managed: true, Host: Host{HostName: "h"}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("", Spec{Alias: "tmp", Group: "prod", Managed: true, Host: Host{HostName: "t"}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetCollapsed([]string{"prod", "data"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.DeleteGroup("prod"); err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(t, tr), []string{"data"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	got := tr.config(t)
	if api := got.Connections["api"]; api.Group != "" || api.Description != "API" {
		t.Errorf("api = %+v, want it ungrouped keeping its description", api)
	}
	if tmp, ok := got.Connections["tmp"]; ok {
		t.Errorf("tmp = %+v, want no metadata left", tmp)
	}
	if want := []string{"data"}; !slices.Equal(got.CollapsedGroups, want) {
		t.Errorf("collapsed = %v, want %v", got.CollapsedGroups, want)
	}
	if hosts := tr.hosts(t); len(hosts) != 2 {
		t.Errorf("Host blocks = %v, want api and tmp kept", hosts)
	}
	if err := tr.cat.DeleteGroup("prod"); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("deleting prod again = %v, want ErrUnknownGroup", err)
	}
}

func TestMoveGroupSetsItsPosition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		position int
		want     []string
	}{
		{"c", 1, []string{"c", "a", "b", "d"}},
		{"a", 4, []string{"b", "c", "d", "a"}},
		{"b", 3, []string{"a", "c", "b", "d"}},
		{"c", 2, []string{"a", "c", "b", "d"}},
		{"b", 2, []string{"a", "b", "c", "d"}},
	} {
		tr := newTree(t, true, "")
		cfg := config.New()
		cfg.Groups = []config.Group{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
		tr.seed(t, cfg)
		if err := tr.cat.MoveGroup(tc.name, tc.position); err != nil {
			t.Fatal(err)
		}
		if got := groupNames(t, tr); !slices.Equal(got, tc.want) {
			t.Errorf("MoveGroup(%q, %d): groups = %v, want %v", tc.name, tc.position, got, tc.want)
		}
	}
}

func TestMoveGroupRefusesUnknownGroupAndPositionOutOfRange(t *testing.T) {
	tr := newTree(t, true, "")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "a"}, {Name: "b"}}
	tr.seed(t, cfg)
	if err := tr.cat.MoveGroup("x", 1); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("MoveGroup(x) = %v, want ErrUnknownGroup", err)
	}
	for _, pos := range []int{0, 3, -1} {
		if err := tr.cat.MoveGroup("a", pos); !errors.Is(err, ErrGroupPosition) {
			t.Errorf("MoveGroup(a, %d) = %v, want ErrGroupPosition", pos, err)
		}
	}
	if got, want := groupNames(t, tr), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestSaveRefusesAGroupWithNoRecord(t *testing.T) {
	tr := newTree(t, true, "Host mine\n  HostName m\n")
	err := tr.cat.Save("", Spec{Alias: "api", Group: "nowhere", Managed: true, Host: Host{HostName: "h"}})
	if !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("Save into an unknown group = %v, want ErrUnknownGroup", err)
	}
	if _, ok := tr.hosts(t)["api"]; ok {
		t.Error("a refused Save wrote api's Host block")
	}
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop", Group: "nowhere"}); !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("Save of metadata into an unknown group = %v, want ErrUnknownGroup", err)
	}
	if got := groupNames(t, tr); len(got) != 0 {
		t.Errorf("groups = %v, want none created", got)
	}
}

func TestRenameGroupRefusesUnknownTakenAndInvalidNames(t *testing.T) {
	tr := newTree(t, true, "")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "staging"}}
	tr.seed(t, cfg)
	for _, tc := range []struct {
		old, name string
		want      error
	}{
		{"nowhere", "x", ErrUnknownGroup},
		{"prod", "staging", ErrGroupExists},
		{"prod", "42", config.ErrGroupName},
		{"prod", "", config.ErrGroupName},
	} {
		if err := tr.cat.RenameGroup(tc.old, tc.name); !errors.Is(err, tc.want) {
			t.Errorf("RenameGroup(%q, %q) = %v, want %v", tc.old, tc.name, err, tc.want)
		}
	}
	if got, want := groupNames(t, tr), []string{"prod", "staging"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}
