package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/workspace"
)

// spaceTree is an app whose config has the default Space (renamed home)
// with Group prod (web) and the loose Connection solo, Space acme with
// Groups prod (db) and data (empty), and Space empty with nothing in it.
// They are Spaces 1, 2 and 4, so 3 is free.
func spaceTree(t *testing.T, in string) (*app, *bytes.Buffer) {
	t.Helper()
	cfg := config.New()
	cfg.Spaces = []config.Space{{Name: "home", Number: 1, Default: true}, {Name: "acme", Number: 2}, {Name: "empty", Number: 4}}
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "prod", Space: "acme"}, {Name: "data", Space: "acme"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
	cfg.Connections["solo"] = config.Connection{Alias: "solo", Description: "loose"}
	cfg.Connections["db"] = config.Connection{Alias: "db", Space: "acme", Group: "prod"}
	return cliTree(t, "Host web\nHost db\nHost solo\nHost other\n", cfg, in)
}

// spaceNumbers returns "<number> <name>" for each Space in a's config, in
// order.
func spaceNumbers(t *testing.T, a *app) []string {
	t.Helper()
	var got []string
	for _, s := range loadCLI(t, a).Spaces {
		got = append(got, strconv.Itoa(s.Number)+" "+s.Name)
	}
	return got
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
		"4  empty                0  0\n"
	if out.String() != want {
		t.Errorf("space list:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestSpaceAddTakesTheLowestFreeNumber(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"space", "add", "prive"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, a), []string{"1 home", "2 acme", "3 prive", "4 empty"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for _, name := range []string{"acme", "7", "a/b", ""} {
		if err := a.execute([]string{"space", "add", name}); err == nil {
			t.Errorf("space add %q succeeded, want it refused", name)
		}
	}
}

func TestSpaceAddWithANumberTakesIt(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"space", "add", "prive", "9"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, a), []string{"1 home", "2 acme", "4 empty", "9 prive"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	for _, args := range [][]string{{"x", "2"}, {"x", "0"}, {"x", "10"}, {"x", "two"}, {"x", "3", "4"}} {
		if err := a.execute(append([]string{"space", "add"}, args...)); err == nil {
			t.Errorf("space add %v succeeded, want it refused", args)
		}
	}
}

func TestSpaceAddRefusesATenthSpace(t *testing.T) {
	a, _ := spaceTree(t, "")
	for _, name := range []string{"s3", "s5", "s6", "s7", "s8", "s9"} {
		if err := a.execute([]string{"space", "add", name}); err != nil {
			t.Fatal(err)
		}
	}
	err := a.execute([]string{"space", "add", "s10"})
	if err == nil || !strings.Contains(err.Error(), "taken") {
		t.Errorf("tenth space: err = %v, want it refused as all numbers taken", err)
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

// space move gives a Space a number, swapping with the Space that had it.
func TestSpaceMoveSwapsOrTakesAFreeNumber(t *testing.T) {
	a, _ := spaceTree(t, "")
	if err := a.execute([]string{"space", "move", "empty", "1"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, a), []string{"1 empty", "2 acme", "4 home"}; !slices.Equal(got, want) {
		t.Errorf("after the swap, spaces = %v, want %v", got, want)
	}
	if err := a.execute([]string{"space", "move", "2", "7"}); err != nil {
		t.Fatal(err)
	}
	if got, want := spaceNumbers(t, a), []string{"1 empty", "4 home", "7 acme"}; !slices.Equal(got, want) {
		t.Errorf("after the move to a free number, spaces = %v, want %v", got, want)
	}
	for _, args := range [][]string{{"acme", "10"}, {"acme", "0"}, {"acme", "first"}, {"nowhere", "1"}, {"3", "1"}} {
		if err := a.execute(append([]string{"space", "move"}, args...)); err == nil {
			t.Errorf("space move %v succeeded, want it refused", args)
		}
	}
	err := a.execute([]string{"space", "move", "acme", "x"})
	if err == nil || strings.Contains(err.Error(), "position") {
		t.Errorf("err = %v, want it refused without the word position", err)
	}
}

// A space command takes a Space by name or by number.
func TestSpaceCommandsTakeASpaceNumber(t *testing.T) {
	a, out := spaceTree(t, "")
	for _, args := range [][]string{
		{"space", "rename", "2", "acme-corp"},
		{"space", "dotfiles", "2", "https://example.com/acme.git"},
		{"space", "delete", "4"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if got, want := spaceNumbers(t, a), []string{"1 home", "2 acme-corp"}; !slices.Equal(got, want) {
		t.Errorf("spaces = %v, want %v", got, want)
	}
	if err := a.execute([]string{"space", "dotfiles", "2"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "https://example.com/acme.git\n" {
		t.Errorf("space dotfiles 2 printed %q, want acme-corp's repo", got)
	}
	err := a.execute([]string{"space", "rename", "3", "x"})
	if err == nil || !strings.Contains(err.Error(), "no Space 3") {
		t.Errorf("space rename 3: err = %v, want no Space 3", err)
	}
}

// --space takes a Space by name or by number, on list, group and key use.
func TestSpaceOptionTakesASpaceNumber(t *testing.T) {
	a, out := spaceTree(t, "")
	if err := a.execute([]string{"list", "--space", "2"}); err != nil {
		t.Fatal(err)
	}
	if want := "db                       [prod]\n"; out.String() != want {
		t.Errorf("list --space 2:\n%s\nwant:\n%s", out.String(), want)
	}
	key := keyFile(t, "id_acme")
	for _, args := range [][]string{
		{"group", "add", "--space", "2", "stats"},
		{"group", "rename", "--space", "2", "prod", "live"},
		{"key", "use", "--space", "2", key},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	cfg := loadCLI(t, a)
	if db := cfg.Connections["db"]; db.Group != "live" {
		t.Errorf("db = %+v, want it in acme's live", db)
	}
	if got := cfg.Groups; !slices.ContainsFunc(got, func(g config.Group) bool { return g.Space == "acme" && g.Name == "stats" }) {
		t.Errorf("groups = %+v, want acme's stats", got)
	}
	if got, _ := a.identityFor("db"); got != key {
		t.Errorf("db's identity = %q, want acme's %q", got, key)
	}
	for _, args := range [][]string{{"list", "--space", "3"}, {"group", "add", "--space", "3", "x"}, {"group", "list", "--space", "9"}} {
		err := a.execute(args)
		if err == nil || !strings.Contains(err.Error(), "no Space") {
			t.Errorf("%v: err = %v, want no Space", args, err)
		}
	}
}

// space list shows a Space beyond the ninth in an older config, which has
// no number, last and without one.
func TestSpaceListShowsUnnumberedSpacesLast(t *testing.T) {
	cfg := config.New()
	cfg.Spaces = []config.Space{{Name: "extra"}}
	want := ""
	for n := 1; n <= config.MaxSpaces; n++ {
		name := "s" + strconv.Itoa(n)
		cfg.Spaces = append(cfg.Spaces, config.Space{Name: name, Number: n, Default: n == 1})
		line := strconv.Itoa(n) + "  " + name + strings.Repeat(" ", 21-len(name)) + "0  0"
		if n == 1 {
			line += "  (default)"
		}
		want += line + "\n"
	}
	want += "-  extra                0  0\n"
	a, out := cliTree(t, "", cfg, "")
	if err := a.execute([]string{"space", "list"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != want {
		t.Errorf("space list:\n%s\nwant:\n%s", out.String(), want)
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
		"4  empty                0  0\n"
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

// The help describes Space numbers, not positions.
func TestHelpDescribesSpaceNumbers(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"space add <name> [<number>]", "space move <space> <number>", "list [--space <space>]"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help is missing %q:\n%s", want, out.String())
		}
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "space") && strings.Contains(line, "position") {
			t.Errorf("help still calls a Space number a position: %q", line)
		}
	}
	a, help := spaceTree(t, "")
	if err := a.execute([]string{"space", "--help"}); err != nil {
		t.Fatal(err)
	}
	if want := "Usage: sshctl " + spaceUsage + "\n"; help.String() != want || strings.Contains(spaceUsage, "position") {
		t.Errorf("space --help = %q", help.String())
	}
}
