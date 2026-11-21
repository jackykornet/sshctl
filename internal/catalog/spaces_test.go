package catalog

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
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

// spaceNumbers lists the Snapshot's Spaces as name:number, in order.
func spaceNumbers(t *testing.T, tr *tree) []string {
	t.Helper()
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range snap.Spaces {
		got = append(got, fmt.Sprintf("%s:%d", s.Name, s.Number))
	}
	return got
}

func TestDeletingASpaceLeavesItsNumberFree(t *testing.T) {
	tr := newTree(t, true, "")
	for _, name := range []string{"acme", "globex", "initech"} {
		if err := tr.cat.AddSpace(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.DeleteSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, tr), []string{"default:1", "globex:3", "initech:4"}; !slices.Equal(got, want) {
		t.Errorf("after deleting acme, spaces = %v, want %v", got, want)
	}
	if err := tr.cat.AddSpace("hooli"); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, tr), []string{"default:1", "hooli:2", "globex:3", "initech:4"}; !slices.Equal(got, want) {
		t.Errorf("a new Space takes the lowest free number: spaces = %v, want %v", got, want)
	}
}

func TestAddSpaceAtPutsTheSpaceAtAFreeNumber(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpaceAt("acme", 4); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, tr), []string{"default:1", "acme:4"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for number, want := range map[int]error{1: ErrSpaceNumberTaken, 4: ErrSpaceNumberTaken, 0: ErrSpaceNumber, 10: ErrSpaceNumber} {
		if err := tr.cat.AddSpaceAt("globex", number); !errors.Is(err, want) {
			t.Errorf("AddSpaceAt(globex, %d) = %v, want %v", number, err, want)
		}
	}
}

func TestAddSpaceRefusesATenthSpace(t *testing.T) {
	tr := newTree(t, true, "")
	for i := 2; i <= 9; i++ {
		if err := tr.cat.AddSpace(fmt.Sprintf("s%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.AddSpace("s10"); !errors.Is(err, ErrSpacesFull) {
		t.Errorf("AddSpace(s10) = %v, want ErrSpacesFull", err)
	}
	if got := spaceNumbers(t, tr); len(got) != 9 || got[8] != "s9:9" {
		t.Errorf("spaces = %v, want default and s2 to s9 numbered 1 to 9", got)
	}
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

func TestMoveSpaceSwapsWithATakenNumberAndMovesToAFreeOne(t *testing.T) {
	tr := newTree(t, true, "")
	for _, name := range []string{"acme", "globex"} {
		if err := tr.cat.AddSpace(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.MoveSpace("globex", 1); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, tr), []string{"globex:1", "acme:2", "default:3"}; !slices.Equal(got, want) {
		t.Errorf("after moving globex to 1, spaces = %v, want %v", got, want)
	}
	if err := tr.cat.MoveSpace("acme", 7); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, tr), []string{"globex:1", "default:3", "acme:7"}; !slices.Equal(got, want) {
		t.Errorf("after moving acme to 7, spaces = %v, want %v", got, want)
	}
	for _, n := range []int{0, 10} {
		if err := tr.cat.MoveSpace("acme", n); !errors.Is(err, ErrSpaceNumber) {
			t.Errorf("MoveSpace(acme, %d) = %v, want ErrSpaceNumber", n, err)
		}
	}
}

// A Space without a number (one past the ninth in an older config) can be
// given a free number, but not one another Space has, which would leave
// that Space without one.
func TestMoveSpaceNumbersASpaceThatHasNone(t *testing.T) {
	spaces := `{"name":"default","default":true}`
	for i := 2; i <= 10; i++ {
		spaces += fmt.Sprintf(`,{"name":"s%d"}`, i)
	}
	tr := newTree(t, true, "")
	if err := os.WriteFile(tr.cfg, []byte(`{"version":4,"spaces":[`+spaces+`],"connections":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.MoveSpace("s10", 2); !errors.Is(err, ErrSpaceNumberTaken) {
		t.Errorf("MoveSpace(s10, 2) = %v, want ErrSpaceNumberTaken", err)
	}
	if err := tr.cat.DeleteSpace("s5"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.MoveSpace("s10", 5); err != nil {
		t.Fatal(err)
	}
	if got := spaceNumbers(t, tr); got[4] != "s10:5" {
		t.Errorf("spaces = %v, want s10 at 5", got)
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

func TestProvisionRepoTakesTheGroupThenTheSpaceThenTheGlobalRepo(t *testing.T) {
	const global, space, group = "https://example.com/global.git", "https://example.com/acme.git", "https://example.com/prod.git"
	for _, tc := range []struct {
		name                 string
		global, space, group string
		grouped              bool
		want                 string
		err                  error
	}{
		{"group wins", global, space, group, true, group, nil},
		{"unset group falls back to the space", global, space, "", true, space, nil},
		{"ungrouped uses the space", global, space, group, false, space, nil},
		{"unset space falls back to global", global, "", "", true, global, nil},
		{"space none refuses despite global", global, config.DotfilesNone, "", true, "", ErrProvisionRefused},
		{"group repo wins over space none", global, config.DotfilesNone, group, true, group, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, true, "Host web\n")
			cfg := config.New()
			cfg.Provision.Dotfiles = tc.global
			cfg.Spaces = append(cfg.Spaces, config.Space{Name: "acme", Dotfiles: tc.space})
			cfg.Groups = []config.Group{{Name: "prod", Space: "acme", Dotfiles: tc.group}}
			c := config.Connection{Alias: "web", Space: "acme"}
			if tc.grouped {
				c.Group = "prod"
			}
			cfg.Connections["web"] = c
			tr.seed(t, cfg)
			got, err := tr.cat.ProvisionRepo("web")
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Errorf("ProvisionRepo = %q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
			if tc.err != nil && !strings.Contains(err.Error(), "space acme") {
				t.Errorf("refusal %q does not name the space", err)
			}
		})
	}
}

func TestSetSpaceDotfilesSavesOnTheSpaceRecord(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpaceDotfiles("acme", "https://example.com/acme.git"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpaceDotfiles("default", config.DotfilesNone); err != nil {
		t.Fatal(err)
	}
	cfg := tr.config(t)
	if got := cfg.Spaces[1].Dotfiles; got != "https://example.com/acme.git" {
		t.Errorf("acme's dotfiles = %q", got)
	}
	if got := cfg.Spaces[0].Dotfiles; got != config.DotfilesNone {
		t.Errorf("the default Space's dotfiles = %q, want none", got)
	}
	if err := tr.cat.SetSpaceDotfiles("nope", "x"); !errors.Is(err, ErrUnknownSpace) {
		t.Errorf("SetSpaceDotfiles(nope) = %v, want ErrUnknownSpace", err)
	}
}

func TestASpacesSelectedIdentityAppliesToItsConnections(t *testing.T) {
	tr := newTree(t, true, "Host web\nHost db\nHost loose\n")
	cfg := config.New()
	cfg.Spaces = append(cfg.Spaces, config.Space{Name: "acme"}, config.Space{Name: "globex"})
	cfg.Connections["db"] = config.Connection{Alias: "db", Space: "acme"}
	cfg.Connections["web"] = config.Connection{Alias: "web", Space: "globex"}
	cfg.SelectedIdentity = "/k/global"
	tr.seed(t, cfg)
	if err := tr.cat.SetSpaceIdentity("acme", "/k/acme"); err != nil {
		t.Fatal(err)
	}
	for alias, want := range map[string]string{"db": "/k/acme", "web": "/k/global", "loose": "/k/global"} {
		if got, err := tr.cat.IdentityFor(alias); err != nil || got != want {
			t.Errorf("IdentityFor(%s) = %q, %v; want %q", alias, got, err, want)
		}
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.SpaceIdentity("acme"); got != "/k/acme" {
		t.Errorf("Snapshot.SpaceIdentity(acme) = %q", got)
	}
	if got := snap.SpaceIdentity("globex"); got != "/k/global" {
		t.Errorf("Snapshot.SpaceIdentity(globex) = %q, want the global one", got)
	}

	// The key follows a renamed Space, and goes with a deleted one.
	if err := tr.cat.RenameSpace("acme", "acme-corp"); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.cat.IdentityFor("db"); got != "/k/acme" {
		t.Errorf("after the rename, IdentityFor(db) = %q, want /k/acme", got)
	}
	if err := tr.cat.SetSpaceIdentity("globex", "/k/globex"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpace("web", "default"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.DeleteSpace("globex"); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).SpaceIdentities; len(got) != 1 || got["acme-corp"] != "/k/acme" {
		t.Errorf("SpaceIdentities = %v, want only acme-corp's", got)
	}

	if err := tr.cat.SetSpaceIdentity("acme-corp", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.cat.IdentityFor("db"); got != "/k/global" {
		t.Errorf("after unsetting, IdentityFor(db) = %q, want the global one", got)
	}
	if err := tr.cat.SetSpaceIdentity("nope", "/k"); !errors.Is(err, ErrUnknownSpace) {
		t.Errorf("SetSpaceIdentity(nope) = %v, want ErrUnknownSpace", err)
	}
}
