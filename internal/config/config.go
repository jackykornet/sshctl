package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Version is the config format written by Save. Version 2 stores Groups
// as records (ADR-0003); version 3 moves the machine-local values to
// state.json (ADR-0004). Older configs are migrated by Load.
const Version = 3

// StateVersion is the state.json format written by Save.
const StateVersion = 1

type Connection struct {
	Alias       string `json:"alias"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
}

// Group is a stored Group record (ADR-0003). Its position in Config.Groups
// is its place in the connection list. Whether it is collapsed is not part
// of the record: that is per machine, in State.CollapsedGroups (ADR-0004).
type Group struct {
	Name string `json:"name"`
}

type Config struct {
	Version int `json:"version"`
	// Groups lists every Group in the user's order. Every Group a
	// Connection names has a record here; Load adds any that is missing.
	Groups      []Group               `json:"groups,omitempty"`
	Connections map[string]Connection `json:"connections"`
	Workspace   Workspace             `json:"workspace,omitzero"`
	Provision   Provision             `json:"provision,omitzero"`
	// State is what belongs to this machine. Save writes it to state.json
	// next to config.json, so a config.json synced from another machine
	// leaves it alone (ADR-0004).
	State `json:"-"`
	// Notices tell the user what Load changed while migrating, such as a
	// renamed Group. They are not saved.
	Notices []string `json:"-"`
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

// Workspace holds optional settings for the full-screen Workspace that are
// the same on every machine. Zero values mean defaults.
type Workspace struct {
	// PrefixKey is the key that starts a Workspace command, such as "ctrl+b".
	PrefixKey string `json:"prefixKey,omitempty"`
	// ScrollbackLines is how many lines of history each tab keeps.
	ScrollbackLines int `json:"scrollbackLines,omitempty"`
}

// State is what belongs to one machine (ADR-0004), saved in state.json.
type State struct {
	// History lists the most recently opened Connections, newest first.
	History []string `json:"history,omitempty"`
	// Keys lists the private keys "sshctl key add" registered.
	Keys []string `json:"keys,omitempty"`
	// DefaultKey is the Selected identity's key path, or "".
	DefaultKey string `json:"defaultKey,omitempty"`
	// CollapsedGroups lists the groups whose members the connection list hides.
	CollapsedGroups []string `json:"collapsedGroups,omitempty"`
	// ListWidth is the width of the connection list in columns.
	ListWidth int `json:"listWidth,omitempty"`
}

// stateFile is state.json's format.
type stateFile struct {
	Version int `json:"version"`
	State
}

// singleFileState is where a config before Version 3 kept the values that
// are now in State.
type singleFileState struct {
	History    []string `json:"history"`
	Keys       []string `json:"keys"`
	DefaultKey string   `json:"defaultKey"`
	Workspace  struct {
		ListWidth       int      `json:"listWidth"`
		CollapsedGroups []string `json:"collapsedGroups"`
	} `json:"workspace"`
}

func (s singleFileState) state() State {
	return State{History: s.History, Keys: s.Keys, DefaultKey: s.DefaultKey, CollapsedGroups: s.Workspace.CollapsedGroups, ListWidth: s.Workspace.ListWidth}
}

// StatePath returns the path of the state.json that goes with the config at
// configPath: the same directory.
func StatePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "state.json")
}

// ErrGroupName is returned by CheckGroupName for a name sshctl refuses.
var ErrGroupName = errors.New("invalid group name")

// CheckGroupName refuses an empty Group name and one made only of digits,
// which would clash with Group numbers.
func CheckGroupName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: a group needs a name", ErrGroupName)
	}
	if allDigits(name) {
		return fmt.Errorf("%w: %q is only digits, which stand for group numbers", ErrGroupName, name)
	}
	return nil
}

func allDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// GroupIndex returns the position of the Group named name in Groups, or -1.
func (c *Config) GroupIndex(name string) int {
	for i, g := range c.Groups {
		if g.Name == name {
			return i
		}
	}
	return -1
}

// SetMetadata sets alias's description and group. Metadata with neither is
// dropped, so an emptied Connection leaves nothing behind.
func (c *Config) SetMetadata(alias, description, group string) {
	if description == "" && group == "" {
		delete(c.Connections, alias)
		return
	}
	c.Connections[alias] = Connection{Alias: alias, Description: description, Group: group}
	c.addMissingGroups()
}

// addMissingGroups gives every Group a Connection names but Groups lacks a
// record, alphabetically after the existing ones. It reports whether it
// added any.
func (c *Config) addMissingGroups() bool {
	var missing []string
	for _, conn := range c.Connections {
		if conn.Group != "" && c.GroupIndex(conn.Group) < 0 && !slices.Contains(missing, conn.Group) {
			missing = append(missing, conn.Group)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		c.Groups = append(c.Groups, Group{Name: name})
	}
	return len(missing) > 0
}

// renameDigitGroups renames every Group made only of digits, in its record,
// its members and the collapsed list, and records a notice for each.
func (c *Config) renameDigitGroups() {
	used := map[string]bool{}
	var digits []string
	note := func(name string) {
		if !used[name] {
			used[name] = true
			if allDigits(name) {
				digits = append(digits, name)
			}
		}
	}
	for _, g := range c.Groups {
		note(g.Name)
	}
	for _, conn := range c.Connections {
		if conn.Group != "" {
			note(conn.Group)
		}
	}
	sort.Strings(digits)
	for _, old := range digits {
		name := "group-" + old
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("group-%s-%d", old, n)
		}
		used[name] = true
		c.renameGroup(old, name)
		c.Notices = append(c.Notices, fmt.Sprintf("renamed group %q to %q: a group name may not be only digits", old, name))
	}
}

// renameGroup renames the Group old to name in its record, its members and
// the collapsed list.
func (c *Config) renameGroup(old, name string) {
	if i := c.GroupIndex(old); i >= 0 {
		c.Groups[i].Name = name
	}
	for alias, conn := range c.Connections {
		if conn.Group == old {
			conn.Group = name
			c.Connections[alias] = conn
		}
	}
	for i, g := range c.CollapsedGroups {
		if g == old {
			c.CollapsedGroups[i] = name
		}
	}
}

// migrateGroups brings c's Groups up to Version 2: a record for every Group
// a Connection names, no all-digit names. It reports whether c changed.
func (c *Config) migrateGroups() bool {
	changed := c.Version < Version
	c.Version = Version
	n := len(c.Notices)
	c.renameDigitGroups()
	if c.addMissingGroups() {
		changed = true
	}
	return changed || len(c.Notices) > n
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

// Load reads the config at path and the state.json next to it (StatePath).
// A missing config is migrated from the first legacy file that exists, and
// a missing state.json is empty. A config from before Version 3 still holds
// the machine-local values; they move to state.json once, unless state.json
// already exists, which then wins.
func Load(path string, legacy []string) (*Config, error) {
	state, haveState, err := loadState(StatePath(path))
	if err != nil {
		return nil, err
	}
	return loadConfig(path, legacy, state, haveState)
}

// loadState reads the state.json at path and reports whether it exists.
func loadState(path string) (State, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var f stateFile
	if err := json.Unmarshal(b, &f); err != nil {
		return State{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	if f.Version < 1 || f.Version > StateVersion {
		return State{}, false, fmt.Errorf("unsupported state version %d in %s", f.Version, path)
	}
	return f.State, true, nil
}

func loadConfig(path string, legacy []string, state State, haveState bool) (*Config, error) {
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
		c.State = state
		if c.Version < 3 && !haveState {
			var old singleFileState
			if e := json.Unmarshal(b, &old); e != nil {
				return nil, fmt.Errorf("read config: %w", e)
			}
			c.State = old.state()
		}
		if c.migrateGroups() {
			if e := Save(path, &c); e != nil {
				return nil, e
			}
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
		c.State = state
		if e = Save(path, c); e != nil {
			return nil, e
		}
		return c, nil
	}
	c := New()
	c.State = state
	return c, nil
}

// Save writes c to the config at path and its State to the state.json next
// to it (StatePath). Each file is replaced through a same-directory
// temporary file and rename, so an interrupted write cannot truncate it.
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
	if err := writeAtomic(path, append(b, '\n'), 0600); err != nil {
		return err
	}
	b, err = json.MarshalIndent(stateFile{Version: StateVersion, State: c.State}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(StatePath(path), append(b, '\n'), 0600)
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
	c.migrateGroups()
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
