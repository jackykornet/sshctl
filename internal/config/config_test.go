package config

import (
	"os"
	"path/filepath"
	"runtime"
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
