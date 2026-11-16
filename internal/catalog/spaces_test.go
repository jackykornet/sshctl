package catalog

import (
	"errors"
	"slices"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// spaceNames lists the Snapshot's Spaces by name, in order.
func spaceNames(t *testing.T, tr *tree) []string {
	t.Helper()
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range snap.Spaces {
		names = append(names, s.Name)
	}
	return names
}

func TestAddSpaceAppendsAnEmptySpace(t *testing.T) {
	tr := newTree(t, true, "")
	for _, name := range []string{"acme", "globex"} {
		if err := tr.cat.AddSpace(name); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := spaceNames(t, tr), []string{config.DefaultSpaceName, "acme", "globex"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for name, want := range map[string]error{"acme": ErrSpaceExists, config.DefaultSpaceName: ErrSpaceExists, "7": config.ErrSpaceName, "a/b": config.ErrSpaceName} {
		if err := tr.cat.AddSpace(name); !errors.Is(err, want) {
			t.Errorf("AddSpace(%q) = %v, want %v", name, err, want)
		}
	}
}

func TestGroupNamesAreUniqueOnlyWithinASpace(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	for _, space := range []string{"", "acme"} {
		if err := tr.cat.AddGroup(space, "prod"); err != nil {
			t.Fatalf("AddGroup(%q, prod) = %v", space, err)
		}
	}
	if err := tr.cat.AddGroup("acme", "prod"); !errors.Is(err, ErrGroupExists) {
		t.Errorf("second prod in acme = %v, want ErrGroupExists", err)
	}
	if err := tr.cat.AddGroup("nowhere", "prod"); !errors.Is(err, ErrUnknownSpace) {
		t.Errorf("AddGroup in an unknown Space = %v, want ErrUnknownSpace", err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HasGroup("", "prod") || !snap.HasGroup("acme", "prod") || len(snap.Groups) != 2 {
		t.Errorf("Groups = %+v, want prod in both Spaces", snap.Groups)
	}
}

func TestSaveAddsAConnectionToASpace(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddGroup("acme", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("", Spec{Alias: "api", Space: "acme", Group: "prod", Managed: true, Host: Host{HostName: "h"}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("", Spec{Alias: "web", Group: "prod", Managed: true, Host: Host{HostName: "w"}}); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("Save into the default Space's missing prod = %v, want ErrUnknownGroup", err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := snap.Entry("api"); e.Space != "acme" || e.Group != "prod" {
		t.Errorf("api = %+v, want acme's prod", e)
	}
}

func TestSetSpaceMovesAConnectionAndLeavesItUngrouped(t *testing.T) {
	tr := newTree(t, true, "Host mine\n  HostName m\n")
	tr.addGroups(t, "home")
	if err := tr.cat.AddSpace("prive"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop", Group: "home"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpace("mine", "prive"); err != nil {
		t.Fatal(err)
	}
	got := tr.config(t).Connections["mine"]
	if got.Space != "prive" || got.Group != "" || got.Description != "laptop" {
		t.Errorf("mine = %+v, want in prive, ungrouped, still described", got)
	}
	if err := tr.cat.SetSpace("mine", config.DefaultSpaceName); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).Connections["mine"]; got.Space != "" {
		t.Errorf("mine = %+v, want back in the default Space", got)
	}
	if err := tr.cat.SetSpace("mine", "nowhere"); !errors.Is(err, ErrUnknownSpace) {
		t.Errorf("SetSpace to an unknown Space = %v, want ErrUnknownSpace", err)
	}
}

func TestDeleteSpaceRefusesTheDefaultAndANonEmptySpace(t *testing.T) {
	tr := newTree(t, true, "Host mine\n  HostName m\n")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddGroup("acme", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpace("mine", "acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.DeleteSpace(config.DefaultSpaceName); !errors.Is(err, ErrDefaultSpace) {
		t.Errorf("deleting the default Space = %v, want ErrDefaultSpace", err)
	}
	err := tr.cat.DeleteSpace("acme")
	if !errors.Is(err, ErrSpaceNotEmpty) {
		t.Fatalf("deleting acme = %v, want ErrSpaceNotEmpty", err)
	}
	if want := `"acme" still has groups prod and connections mine: the space is not empty`; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if err := tr.cat.DeleteGroup("acme", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpace("mine", ""); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetActiveSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.DeleteSpace("acme"); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Spaces) != 1 || snap.ActiveSpace != "" {
		t.Errorf("Spaces %+v, active %q; want only the default Space, active", snap.Spaces, snap.ActiveSpace)
	}
}

func TestRenameSpaceKeepsItsGroupsConnectionsAndActiveState(t *testing.T) {
	tr := newTree(t, true, "Host api\n  HostName a\n")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddGroup("acme", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("api", Spec{Alias: "api", Space: "acme", Group: "prod"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetCollapsed([]string{config.CollapsedKey("acme", "prod")}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetActiveSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.RenameSpace("acme", "globex"); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := snap.Entry("api"); e.Space != "globex" || e.Group != "prod" || !snap.HasGroup("globex", "prod") {
		t.Errorf("api = %+v, Groups %+v; want both in globex", e, snap.Groups)
	}
	if snap.ActiveSpace != "globex" || !slices.Equal(snap.Collapsed, []string{"globex/prod"}) {
		t.Errorf("active %q, collapsed %v; want globex, [globex/prod]", snap.ActiveSpace, snap.Collapsed)
	}
	if err := tr.cat.RenameSpace("", "homelab"); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNames(t, tr), []string{"homelab", "globex"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	if err := tr.cat.RenameSpace("homelab", "globex"); !errors.Is(err, ErrSpaceExists) {
		t.Errorf("renaming onto a taken name = %v, want ErrSpaceExists", err)
	}
}

func TestMoveSpaceReorders(t *testing.T) {
	tr := newTree(t, true, "")
	for _, name := range []string{"acme", "globex"} {
		if err := tr.cat.AddSpace(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.MoveSpace("globex", 1); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNames(t, tr), []string{"globex", config.DefaultSpaceName, "acme"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for _, pos := range []int{0, 4} {
		if err := tr.cat.MoveSpace("acme", pos); !errors.Is(err, ErrSpacePosition) {
			t.Errorf("MoveSpace(acme, %d) = %v, want ErrSpacePosition", pos, err)
		}
	}
}

func TestMoveGroupCountsWithinItsSpace(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	for _, g := range [][2]string{{"", "a"}, {"acme", "x"}, {"", "b"}, {"acme", "y"}} {
		if err := tr.cat.AddGroup(g[0], g[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.MoveGroup("acme", "y", 1); err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(t, tr), []string{"a", "y", "b", "x"}; !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v (acme's swapped, the default Space's in place)", got, want)
	}
	if err := tr.cat.MoveGroup("acme", "y", 3); !errors.Is(err, ErrGroupPosition) {
		t.Errorf("MoveGroup past acme's 2 Groups = %v, want ErrGroupPosition", err)
	}
}

func TestSetGroupFindsTheGroupInTheConnectionsSpace(t *testing.T) {
	tr := newTree(t, true, "Host api\n  HostName a\n")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddGroup("", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpace("api", "acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetGroup("api", "prod"); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("SetGroup to the default Space's prod = %v, want ErrUnknownGroup", err)
	}
	if err := tr.cat.AddGroup("acme", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetGroup("api", "prod"); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).Connections["api"]; got.Space != "acme" || got.Group != "prod" {
		t.Errorf("api = %+v, want acme's prod", got)
	}
}

func TestProvisionRepoUsesTheGroupOfTheConnectionsSpace(t *testing.T) {
	tr := newTree(t, true, "Host api\n  HostName a\n")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	for _, space := range []string{"", "acme"} {
		if err := tr.cat.AddGroup(space, "prod"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.SetGroupDotfiles("", "prod", config.DotfilesNone); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetGroupDotfiles("acme", "prod", "https://example.com/acme.git"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("api", Spec{Alias: "api", Space: "acme", Group: "prod"}); err != nil {
		t.Fatal(err)
	}
	if repo, err := tr.cat.ProvisionRepo("api"); err != nil || repo != "https://example.com/acme.git" {
		t.Errorf("ProvisionRepo = %q, %v; want acme's prod repo", repo, err)
	}
}

func TestSnapshotResolvesSpaceNamesAndListsASpacesGroups(t *testing.T) {
	snap := Snapshot{
		Spaces: []Space{{Name: "home", Default: true}, {Name: "acme"}},
		Groups: []Group{{Name: "prod"}, {Name: "prod", Space: "acme"}, {Name: "web"}, {Name: "data", Space: "acme"}},
	}
	for name, want := range map[string]string{"home": "", "": "", "acme": "acme"} {
		if ref, ok := snap.SpaceRef(name); !ok || ref != want {
			t.Errorf("SpaceRef(%q) = %q, %v; want %q, true", name, ref, ok, want)
		}
	}
	if _, ok := snap.SpaceRef("nowhere"); ok {
		t.Error("SpaceRef(nowhere) found a Space")
	}
	if got, want := snap.GroupNames(""), []string{"prod", "web"}; !slices.Equal(got, want) {
		t.Errorf("GroupNames(home) = %v, want %v", got, want)
	}
	if got, want := snap.GroupNames("acme"), []string{"prod", "data"}; !slices.Equal(got, want) {
		t.Errorf("GroupNames(acme) = %v, want %v", got, want)
	}
}
