package config

import (
	"os"
	"path/filepath"
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
