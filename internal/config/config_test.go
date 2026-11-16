package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestMigrateLegacyConnections(t *testing.T) {
	data := []byte(`{"connections":[{"Name":"prod","Description":"Production","Group":"work"}]}`)
	c, err := migrate(data)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Connections["prod"]
	if got.Description != "Production" || got.Group != "work" {
		t.Fatalf("unexpected migration: %#v", got)
	}
}
func TestLoadMigratesWithoutDeletingSource(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "connections.json")
	dest := filepath.Join(dir, "sshctl", "config.json")
	if err := os.WriteFile(legacy, []byte(`[{"alias":"box","description":"dev"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dest, []string{legacy})
	if err != nil {
		t.Fatal(err)
	}
	if c.Connections["box"].Description != "dev" {
		t.Fatalf("migration lost data: %#v", c)
	}
	if _, err = os.Stat(legacy); err != nil {
		t.Fatalf("legacy source removed: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not expose POSIX permission bits through FileMode.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions = %o", info.Mode().Perm())
	}
}

// writeConfig writes body as the sshctl config in a new directory and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMigratesGroupNamesToGroupRecords(t *testing.T) {
	path := writeConfig(t, `{"version":1,"connections":{
		"web":{"alias":"web","group":"prod"},
		"db":{"alias":"db","group":"prod"},
		"ci":{"alias":"ci","group":"build"},
		"old":{"alias":"old","group":"2"},
		"older":{"alias":"older","group":"group-2"},
		"loose":{"alias":"loose","description":"no group"}},
		"workspace":{"collapsedGroups":["2","prod"]}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(c), []string{"build", "group-2", "group-2-2", "prod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	if got := c.Connections["old"].Group; got != "group-2-2" {
		t.Errorf("old's group = %q, want the renamed group-2-2", got)
	}
	if got := c.Connections["older"].Group; got != "group-2" {
		t.Errorf("older's group = %q, want group-2 kept", got)
	}
	if got, want := c.CollapsedGroups, []string{"group-2-2", "prod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("collapsed = %v, want %v", got, want)
	}
	if len(c.Notices) != 1 || !strings.Contains(c.Notices[0], `"2"`) || !strings.Contains(c.Notices[0], `"group-2-2"`) {
		t.Errorf("notices = %q, want one naming the rename", c.Notices)
	}
	again, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != Version || len(again.Notices) != 0 || !reflect.DeepEqual(groupNames(again), groupNames(c)) {
		t.Errorf("reloaded %+v, want the migration saved once", again)
	}
}

func TestLoadKeepsStoredGroupOrderAndEmptyGroups(t *testing.T) {
	path := writeConfig(t, `{"version":2,"groups":[{"name":"zeta"},{"name":"alpha"},{"name":"empty"}],
		"connections":{"web":{"alias":"web","group":"alpha"},"x":{"alias":"x","group":"zeta"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(c), []string{"zeta", "alpha", "empty"}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestLoadGivesAGroupNamedOnlyByAConnectionARecord(t *testing.T) {
	path := writeConfig(t, `{"version":2,"groups":[{"name":"zeta"}],
		"connections":{"b":{"alias":"b","group":"beta"},"a":{"alias":"a","group":"alpha"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(c), []string{"zeta", "alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestMigrateLegacyConnectionsCreatesGroups(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(legacy, []byte(`[{"alias":"b","group":"web"},{"alias":"a","group":"db"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filepath.Join(dir, "sshctl", "config.json"), []string{legacy})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := groupNames(c), []string{"db", "web"}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestCheckGroupName(t *testing.T) {
	for _, tt := range []struct {
		name string
		ok   bool
	}{{"prod", true}, {"web-2", true}, {"2", false}, {"007", false}, {"", false}} {
		if err := CheckGroupName(tt.name); (err == nil) != tt.ok {
			t.Errorf("CheckGroupName(%q) = %v, want ok %v", tt.name, err, tt.ok)
		}
	}
}

func groupNames(c *Config) []string {
	var out []string
	for _, g := range c.Groups {
		out = append(out, g.Name)
	}
	return out

}

// symlinkOrSkip links link to target, skipping where the OS cannot make
// symlinks (unprivileged Windows).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot make symlinks here: %v", err)
	}
}

func TestSaveWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	home := filepath.Join(dir, "home")
	for _, d := range []string{source, home} {
		if err := os.Mkdir(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(source, "config.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "config.json")
	symlinkOrSkip(t, target, link)

	c := New()
	c.Connections["box"] = Connection{Description: "dev"}
	if err := Save(link, c); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v, %v", fi, err)
	}
	got, err := Load(target, nil)
	if err != nil || got.Connections["box"].Description != "dev" {
		t.Fatalf("target not updated: %#v, %v", got, err)
	}
	// state.json belongs to this machine, so it is written next to the
	// link, not into chezmoi's source directory.
	if entries, _ := os.ReadDir(home); len(entries) != 2 || entries[1].Name() != "state.json" {
		t.Errorf("files next to the link = %v, want the link and state.json", entries)
	}
	if entries, _ := os.ReadDir(source); len(entries) != 1 {
		t.Errorf("files in the source = %v, want only config.json", entries)
	}
}

// A state.json the user linked elsewhere is written through the link too.
func TestSaveWritesStateThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	target := filepath.Join(dir, "elsewhere-state.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, target, StatePath(path))

	c := New()
	c.History = []string{"web"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(StatePath(path)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("state.json link replaced: %v, %v", fi, err)
	}
	b, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(b), `"web"`) {
		t.Fatalf("link target = %q, %v; want the History", b, err)
	}
}

func TestSaveRefusesDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "config.json")
	symlinkOrSkip(t, filepath.Join(dir, "missing", "config.json"), link)
	err := Save(link, New())
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), link) {
		t.Fatalf("Save through dangling link = %v, want a clear error", err)
	}
	if fi, _ := os.Lstat(link); fi == nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling link was replaced")
	}
}

func TestGroupDotfilesSurviveARenameAndASaveRoundTrip(t *testing.T) {
	path := writeConfig(t, `{"version":2,"groups":[{"name":"7","dotfiles":"https://example.com/d.git"},{"name":"off","dotfiles":"none"}],
		"connections":{"web":{"alias":"web","group":"7"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Group{{Name: "group-7", Dotfiles: "https://example.com/d.git"}, {Name: "off", Dotfiles: DotfilesNone}}
	if !reflect.DeepEqual(c.Groups, want) {
		t.Errorf("groups = %+v, want %+v", c.Groups, want)
	}
	again, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Groups, want) {
		t.Errorf("reloaded groups = %+v, want %+v", again.Groups, want)
	}
}

// fileKeys returns the top-level keys of the JSON object in path, and the
// keys of its "workspace" object prefixed "workspace.".
func fileKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k, v := range m {
		keys = append(keys, k)
		if k == "workspace" {
			var w map[string]any
			if err := json.Unmarshal(v, &w); err != nil {
				t.Fatal(err)
			}
			for wk := range w {
				keys = append(keys, "workspace."+wk)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func TestLoadMovesMachineLocalValuesOutOfASingleFileConfig(t *testing.T) {
	path := writeConfig(t, `{"version":2,"groups":[{"name":"prod"}],
		"connections":{"web":{"alias":"web","description":"site","group":"prod"}},
		"history":["web","db"],"keys":["/k/id","/k/other"],"defaultKey":"/k/id",
		"workspace":{"prefixKey":"ctrl+a","listWidth":40,"collapsedGroups":["prod"]},
		"provision":{"dotfiles":"https://example.com/dotfiles.git"}}`)
	want := New()
	want.Groups = []Group{{Name: "prod"}}
	want.Connections["web"] = Connection{Alias: "web", Description: "site", Group: "prod"}
	want.Workspace.PrefixKey = "ctrl+a"
	want.Provision.Dotfiles = "https://example.com/dotfiles.git"
	machineLocal(want)

	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("Load = %+v, want %+v", c, want)
	}
	if got, want := fileKeys(t, path), []string{"connections", "groups", "provision", "version", "workspace", "workspace.prefixKey"}; !reflect.DeepEqual(got, want) {
		t.Errorf("config.json keys after migration = %v, want %v", got, want)
	}
	again, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Errorf("reloaded %+v, want %+v", again, want)
	}
}

func TestReplacingConfigKeepsThisMachinesState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := New()
	c.Groups = []Group{{Name: "prod"}}
	machineLocal(c)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	for name, other := range map[string]string{
		"current": `{"version":3,"groups":[{"name":"prod"},{"name":"lab"}],"connections":{"pi":{"alias":"pi","group":"lab"}}}`,
		"single file": `{"version":2,"groups":[{"name":"prod"},{"name":"lab"}],"connections":{"pi":{"alias":"pi","group":"lab"}},
			"history":["pi"],"defaultKey":"/other/key","workspace":{"collapsedGroups":["lab"],"listWidth":10}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(other), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.State, c.State) {
				t.Errorf("state = %+v, want this machine's %+v", got.State, c.State)
			}
			if got.Connections["pi"].Group != "lab" || groupNames(got)[1] != "lab" {
				t.Errorf("config = %+v, want the other machine's Connections and Groups", got)
			}
		})
	}
}

func TestLoadTreatsAMissingStateFileAsEmpty(t *testing.T) {
	path := writeConfig(t, `{"version":3,"connections":{"web":{"alias":"web","description":"site"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.State, State{}) || c.Connections["web"].Description != "site" {
		t.Errorf("Load = %+v, want the config with empty state", c)
	}
	if _, err := os.Stat(StatePath(path)); !os.IsNotExist(err) {
		t.Errorf("Load wrote state.json (%v); only a save should", err)
	}
}

// machineLocal fills every machine-local value in c.
func machineLocal(c *Config) {
	c.History = []string{"web", "db"}
	c.Keys = []string{"/k/id", "/k/other"}
	c.SelectedIdentity = "/k/id"
	c.CollapsedGroups = []string{"prod"}
	c.ListWidth = 40
}

func TestSaveKeepsMachineLocalStateInStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := New()
	c.Groups = []Group{{Name: "prod"}}
	c.Connections["web"] = Connection{Alias: "web", Description: "site", Group: "prod"}
	c.Workspace.PrefixKey = "ctrl+a"
	c.Provision.Dotfiles = "https://example.com/dotfiles.git"
	machineLocal(c)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if got, want := fileKeys(t, path), []string{"connections", "groups", "provision", "version", "workspace", "workspace.prefixKey"}; !reflect.DeepEqual(got, want) {
		t.Errorf("config.json keys = %v, want %v", got, want)
	}
	if got, want := fileKeys(t, filepath.Join(dir, "state.json")), []string{"collapsedGroups", "defaultKey", "history", "keys", "listWidth", "version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("state.json keys = %v, want %v", got, want)
	}
	got, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Errorf("Load = %+v, want %+v", got, c)
	}
}
