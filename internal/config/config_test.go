package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	if got, want := c.Workspace.CollapsedGroups, []string{"group-2-2", "prod"}; !reflect.DeepEqual(got, want) {
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
	if entries, _ := os.ReadDir(home); len(entries) != 1 {
		t.Errorf("files next to the link = %v, want only the link", entries)
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
