package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHome points the user's home directory at a temporary one on every OS.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestPathsUseDotConfigOnEveryOS(t *testing.T) {
	home := setHome(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	got, _, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "sshctl", "config.json"); got != want {
		t.Fatalf("Paths() = %q, want %q", got, want)
	}
}

func TestPathsFollowXDGConfigHome(t *testing.T) {
	setHome(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got, _, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "sshctl", "config.json"); got != want {
		t.Fatalf("Paths() = %q, want %q", got, want)
	}
}

func TestConfigDirs(t *testing.T) {
	home := filepath.Join("h", "jo")
	appData := filepath.Join("h", "jo", "AppData", "Roaming")
	xdg := t.TempDir() // absolute on every OS
	tests := []struct {
		name, goos, xdg  string
		wantDir, wantOld string
	}{
		{"linux", "linux", "", filepath.Join(home, ".config", "sshctl"), ""},
		{"linux xdg", "linux", xdg, filepath.Join(xdg, "sshctl"), ""},
		{"relative xdg is ignored", "linux", "rel", filepath.Join(home, ".config", "sshctl"), ""},
		{"macos", "darwin", "", filepath.Join(home, ".config", "sshctl"), filepath.Join(home, "Library", "Application Support", "sshctl")},
		{"windows", "windows", "", filepath.Join(home, ".config", "sshctl"), filepath.Join(appData, "sshctl")},
		{"windows xdg", "windows", xdg, filepath.Join(xdg, "sshctl"), filepath.Join(appData, "sshctl")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"XDG_CONFIG_HOME": tt.xdg, "AppData": appData}
			dir, old := configDirs(tt.goos, home, func(k string) string { return env[k] })
			if dir != tt.wantDir || old != tt.wantOld {
				t.Fatalf("configDirs = %q, %q; want %q, %q", dir, old, tt.wantDir, tt.wantOld)
			}
		})
	}
}

// writeFiles writes each name → body into dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMoveOldMovesConfigAndStateOnce(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "Library", "Application Support", "sshctl")
	path := filepath.Join(root, ".config", "sshctl", "config.json")
	writeFiles(t, old, map[string]string{"config.json": `{"version":3}`, "state.json": `{"version":1}`})

	notice, err := moveOld(old, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, old) || !strings.Contains(notice, filepath.Dir(path)) {
		t.Errorf("notice = %q, want it to name both folders", notice)
	}
	if got := readFile(t, path); got != `{"version":3}` {
		t.Errorf("config.json = %q", got)
	}
	if got := readFile(t, StatePath(path)); got != `{"version":1}` {
		t.Errorf("state.json = %q", got)
	}
	for _, name := range []string{"config.json", "state.json"} {
		if _, err := os.Stat(filepath.Join(old, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("old %s still there (err %v): want it moved, not copied", name, err)
		}
	}

	again, err := moveOld(old, path)
	if err != nil || again != "" {
		t.Errorf("second moveOld = %q, %v; want no notice", again, err)
	}
}

func TestMoveOldLeavesBothAloneWhenTheNewConfigExists(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "AppData", "sshctl")
	path := filepath.Join(root, ".config", "sshctl", "config.json")
	writeFiles(t, old, map[string]string{"config.json": "old", "state.json": "old state"})
	writeFiles(t, filepath.Dir(path), map[string]string{"config.json": "new"})

	notice, err := moveOld(old, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, old) || !strings.Contains(notice, filepath.Dir(path)) {
		t.Errorf("notice = %q, want a hint naming both folders", notice)
	}
	if readFile(t, path) != "new" || readFile(t, filepath.Join(old, "config.json")) != "old" || readFile(t, filepath.Join(old, "state.json")) != "old state" {
		t.Error("a file changed; want the new config to win and the old folder left as it is")
	}
	if _, err := os.Stat(StatePath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state.json was moved next to the existing new config (err %v)", err)
	}
}

func TestMoveOldDoesNothingWithoutAnOldConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".config", "sshctl", "config.json")
	for _, old := range []string{"", filepath.Join(root, "missing")} {
		notice, err := moveOld(old, path)
		if err != nil || notice != "" {
			t.Errorf("moveOld(%q) = %q, %v; want nothing", old, notice, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config folder created with nothing to move (err %v)", err)
	}
}

func TestMoveOldThenLoadReadsTheMovedConfig(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old", "sshctl")
	path := filepath.Join(root, ".config", "sshctl", "config.json")
	writeFiles(t, old, map[string]string{
		"config.json": `{"version":3,"connections":{"box":{"alias":"box","description":"dev"}}}`,
		"state.json":  `{"version":1,"history":["box"]}`,
	})
	if _, err := moveOld(old, path); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Connections["box"].Description != "dev" || len(c.History) != 1 {
		t.Fatalf("Load after move = %#v", c)
	}
}
