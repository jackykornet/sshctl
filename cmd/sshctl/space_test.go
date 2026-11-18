package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/workspace"
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

func TestSpaceDotfilesSetsShowsAndUnsetsASpacesRepo(t *testing.T) {
	a, out := spaceTree(t, "")
	const repo = "https://example.com/acme.git"
	for _, args := range [][]string{{"acme", repo}, {"home", "none"}} {
		if err := a.execute(append([]string{"space", "dotfiles"}, args...)); err != nil {
			t.Fatalf("space dotfiles %v: %v", args, err)
		}
	}
	cfg := loadCLI(t, a)
	if cfg.Spaces[1].Dotfiles != repo || cfg.Spaces[0].Dotfiles != config.DotfilesNone {
		t.Fatalf("Spaces = %+v, want acme's repo and home's none", cfg.Spaces)
	}
	if err := a.execute([]string{"space", "dotfiles", "acme"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != repo+"\n" {
		t.Errorf("space dotfiles acme printed %q, want the repo", got)
	}
	if err := a.execute([]string{"space", "dotfiles", "acme", "--unset"}); err != nil {
		t.Fatal(err)
	}
	if got := loadCLI(t, a).Spaces[1].Dotfiles; got != "" {
		t.Errorf("after --unset, acme's dotfiles = %q", got)
	}
	for _, args := range [][]string{{"nope", repo}, {"acme", repo, "--unset"}, {}} {
		if err := a.execute(append([]string{"space", "dotfiles"}, args...)); err == nil {
			t.Errorf("space dotfiles %v did not fail", args)
		}
	}
}

// keyFile creates an empty private key file and returns its path.
func keyFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKeyUseSpaceSelectsTheKeyTheSpacesConnectionsUse(t *testing.T) {
	a, _ := spaceTree(t, "")
	global, acme := keyFile(t, "id_global"), keyFile(t, "id_acme")
	if err := a.execute([]string{"key", "use", global}); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"key", "use", "--space", "acme", acme}); err != nil {
		t.Fatal(err)
	}
	for alias, want := range map[string]string{"db": acme, "web": global} {
		got, err := a.sessionCommand(alias, workspace.Launch{})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"ssh", "-i", want, alias}) {
			t.Errorf("sessionCommand(%s) = %q, want ssh -i %s", alias, got, want)
		}
	}
	if err := a.execute([]string{"key", "use", "--space", "acme", "none"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.identityFor("db"); got != global {
		t.Errorf("after none, db's identity = %q, want the global %q", got, global)
	}
	if err := a.execute([]string{"key", "use", "--space", "nope", acme}); err == nil {
		t.Error("key use --space nope did not fail")
	}
}

func TestProvisionUsesTheSpacesIdentityAndRepo(t *testing.T) {
	cfg := config.New()
	cfg.SelectedIdentity = "/k/global"
	cfg.SpaceIdentities = map[string]string{"acme": "/k/acme"}
	cfg.Spaces = append(cfg.Spaces, config.Space{Name: "acme", Dotfiles: dotfiles})
	cfg.Connections["db"] = config.Connection{Alias: "db", Space: "acme"}
	a, _ := cliTree(t, "Host db\n", cfg, "")
	r := &sshRecorder{}
	a.runSSH = r.run
	if err := a.execute([]string{"provision", "db"}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || !slices.Equal(r.calls[1][:4], []string{"ssh", "-i", "/k/acme", "db"}) {
		t.Fatalf("ssh calls %q, want the script run with acme's key", r.calls)
	}
	if !strings.Contains(r.calls[1][4], dotfiles) {
		t.Errorf("the script does not apply acme's repo %s", dotfiles)
	}
}

func TestSpaceListShowsASpacesDotfilesAndKey(t *testing.T) {
	a, out := spaceTree(t, "")
	key := keyFile(t, "id_acme")
	for _, args := range [][]string{
		{"space", "dotfiles", "acme", "https://example.com/acme.git"},
		{"key", "use", "--space", "acme", key},
		{"space", "list"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := "" +
		"1  home                 1  3  (default)\n" +
		"2  acme                 2  1  dotfiles https://example.com/acme.git  key " + key + "\n" +
		"3  empty                0  0\n"
	if out.String() != want {
		t.Errorf("space list:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestProvisionCanSaveTheAskedRepoForTheSpace(t *testing.T) {
	const repo = "https://example.com/acme.git"
	cfg := config.New()
	cfg.Spaces = append(cfg.Spaces, config.Space{Name: "acme"})
	cfg.Connections["db"] = config.Connection{Alias: "db", Space: "acme"}
	a, out := cliTree(t, "Host db\n", cfg, repo+"\ns\n")
	a.runSSH = (&sshRecorder{}).run
	if err := a.execute([]string{"provision", "db"}); err != nil {
		t.Fatal(err)
	}
	if want := "Save for: (g)lobal, (s) space acme, (o)nce: "; !strings.Contains(out.String(), want) {
		t.Errorf("output %q does not offer %q", out.String(), want)
	}
	if got := loadCLI(t, a).Spaces[1].Dotfiles; got != repo {
		t.Errorf("acme's dotfiles = %q, want %s", got, repo)
	}
}

func TestProvisionDefaultListsSpaceAndGroupSettingsBySpace(t *testing.T) {
	a, out := spaceTree(t, "")
	if err := a.execute([]string{"space", "dotfiles", "acme", "none"}); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"provision", "default"}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"global: unset\n" +
		"space home: unset\n" +
		"space acme: none (Provision refused)\n" +
		"space empty: unset\n" +
		"group prod: unset\n" +
		"group acme/prod: unset\n" +
		"group acme/data: unset\n"
	if out.String() != want {
		t.Errorf("provision default:\n%s\nwant:\n%s", out.String(), want)
	}
}
