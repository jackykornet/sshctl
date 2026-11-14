package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Version = 1

type Connection struct {
	Alias       string `json:"alias"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
}

type Config struct {
	Version     int                   `json:"version"`
	Connections map[string]Connection `json:"connections"`
	History     []string              `json:"history,omitempty"`
	Keys        []string              `json:"keys,omitempty"`
	DefaultKey  string                `json:"defaultKey,omitempty"`
	Workspace   Workspace             `json:"workspace,omitzero"`
	Provision   Provision             `json:"provision,omitzero"`
}

// DotfilesNone is the dotfiles setting that refuses Provision.
const DotfilesNone = "none"

// Provision holds the settings Provision reads. They are the same on every
// machine, so they belong with the synced settings (ADR-0004).
type Provision struct {
	// Dotfiles is the global dotfiles repo URL, DotfilesNone to refuse
	// Provision, or "" when unset.
	Dotfiles string `json:"dotfiles,omitempty"`
}

// Workspace holds optional settings for the full-screen Workspace. Zero values mean defaults.
type Workspace struct {
	// PrefixKey is the key that starts a Workspace command, such as "ctrl+b".
	PrefixKey string `json:"prefixKey,omitempty"`
	// ListWidth is the width of the connection list in columns.
	ListWidth int `json:"listWidth,omitempty"`
	// ScrollbackLines is how many lines of history each tab keeps.
	ScrollbackLines int `json:"scrollbackLines,omitempty"`
	// CollapsedGroups lists the groups whose members the connection list hides.
	CollapsedGroups []string `json:"collapsedGroups,omitempty"`
}

// SetMetadata sets alias's description and group. Metadata with neither is
// dropped, so an emptied Connection leaves nothing behind.
func (c *Config) SetMetadata(alias, description, group string) {
	if description == "" && group == "" {
		delete(c.Connections, alias)
		return
	}
	c.Connections[alias] = Connection{Alias: alias, Description: description, Group: group}
}

// historyLen is how many Connections History keeps.
const historyLen = 20

// RecordOpen moves alias to the front of History, keeping the most recent
// historyLen.
func (c *Config) RecordOpen(alias string) {
	h := []string{alias}
	for _, a := range c.History {
		if a != alias && len(h) < historyLen {
			h = append(h, a)
		}
	}
	c.History = h
}

func New() *Config { return &Config{Version: Version, Connections: map[string]Connection{}} }

func Paths() (string, []string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", nil, err
	}
	primary := filepath.Join(dir, "sshctl", "config.json")
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, err
	}
	legacy := []string{filepath.Join(home, ".sshctl", "connections.json"), filepath.Join(home, ".sshyeah", "connections.json")}
	if runtime.GOOS == "windows" {
		legacy = append(legacy, filepath.Join(home, "sshctl", "connections.json"), filepath.Join(home, "sshyeah", "connections.json"))
	}
	return primary, legacy, nil
}

func Load(path string, legacy []string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		var c Config
		if e := json.Unmarshal(b, &c); e != nil {
			return nil, fmt.Errorf("read config: %w", e)
		}
		if c.Version < 1 || c.Version > Version {
			return nil, fmt.Errorf("unsupported config version %d", c.Version)
		}
		if c.Connections == nil {
			c.Connections = map[string]Connection{}
		}
		return &c, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, candidate := range legacy {
		b, e := os.ReadFile(candidate)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		c, e := migrate(b)
		if e != nil {
			return nil, fmt.Errorf("migrate %s: %w", candidate, e)
		}
		if e = Save(path, c); e != nil {
			return nil, e
		}
		return c, nil
	}
	return New(), nil
}

// Save uses a same-directory temporary file and rename so an interrupted write cannot truncate config.
func Save(path string, c *Config) error {
	if c.Version == 0 {
		c.Version = Version
	}
	if c.Connections == nil {
		c.Connections = map[string]Connection{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), 0600)
}

// writeAtomic replaces path through a temporary file in the same directory,
// so an interrupted write never leaves it truncated. When path is a symlink
// (chezmoi can link it into its source directory, see ADR-0004) the write
// goes to the symlink's target and the symlink stays.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	path, err := resolveSymlink(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sshctl-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// resolveSymlink returns the file a save to path should replace: path itself,
// or the final target when path is a symlink. A symlink to nothing is an error,
// so the save does not silently replace the link with a plain file.
func resolveSymlink(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		dest, _ := os.Readlink(path)
		return "", fmt.Errorf("%s is a symlink to %s, which does not exist", path, dest)
	}
	return target, err
}

func migrate(b []byte) (*Config, error) {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	c := New()
	var entries []any
	switch x := raw.(type) {
	case []any:
		entries = x
	case map[string]any:
		for _, key := range []string{"connections", "Connections", "hosts", "Hosts"} {
			if a, ok := x[key].([]any); ok {
				entries = a
				break
			}
		}
		if entries == nil {
			for alias, val := range x {
				if alias == "version" {
					continue
				}
				if m, ok := val.(map[string]any); ok {
					m["alias"] = alias
					entries = append(entries, m)
				}
			}
		}
	default:
		return nil, fmt.Errorf("expected an object or array")
	}
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		alias := field(m, "alias", "Alias", "name", "Name", "host", "Host")
		if alias == "" {
			continue
		}
		c.Connections[alias] = Connection{Alias: alias, Description: field(m, "description", "Description", "desc", "Desc"), Group: field(m, "group", "Group")}
	}
	return c, nil
}
func field(m map[string]any, names ...string) string {
	for k, v := range m {
		for _, n := range names {
			if strings.EqualFold(k, n) {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}
