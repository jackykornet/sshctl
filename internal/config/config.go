package config

import (
	"encoding/json"
	"errors"
	"fmt"
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
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sshctl-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
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
