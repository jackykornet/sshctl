package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/sshctl/sshctl/internal/atomicfile"
)

// Version is the config format written by Save. Version 2 stores Groups
// as records (ADR-0003); version 3 moves the machine-local values to
// state.json (ADR-0004); version 4 adds Spaces (ADR-0005). Older configs
// are migrated by Load.
const Version = 4

// StateVersion is the state.json format written by Save.
const StateVersion = 1

type Connection struct {
	Alias       string `json:"alias"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
	// Space is the Space the Connection belongs to, "" for the default
	// Space. Its Group is a Group of that Space.
	Space string `json:"space,omitempty"`
}

// Group is a stored Group record (ADR-0003). Its position in Config.Groups
// is its place in the connection list. Whether it is collapsed is not part
// of the record: that is per machine, in State.CollapsedGroups (ADR-0004).
type Group struct {
	Name string `json:"name"`
	// Space is the Space the Group belongs to, "" for the default Space.
	// A Group name is unique only within its Space.
	Space string `json:"space,omitempty"`
	// Dotfiles is the dotfiles repo URL Provision applies to the Group's
	// Connections, DotfilesNone to refuse Provision for them, or "" when
	// unset, so the global Provision.Dotfiles applies.
	Dotfiles string `json:"dotfiles,omitempty"`
}

type Config struct {
	Version int `json:"version"`
	// Spaces lists every Space in the user's order; exactly one is the
	// default Space. Load adds any that is missing.
	Spaces []Space `json:"spaces,omitempty"`
	// Groups lists every Group in the user's order, the Groups of each
	// Space in that Space's order. Every Group a Connection names has a
	// record here; Load adds any that is missing.
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
	// SelectedIdentity is the Selected identity's key path, or "".
	SelectedIdentity string `json:"defaultKey,omitempty"`
	// SpaceIdentities is the Selected identity of each Space that selects
	// its own, by how Connections refer to the Space ("" for the default
	// Space). Key paths are per machine, so it is here rather than on the
	// Space record (ADR-0005).
	SpaceIdentities map[string]string `json:"spaceKeys,omitempty"`
	// CollapsedGroups lists the groups whose members the connection list
	// hides, by CollapsedKey.
	CollapsedGroups []string `json:"collapsedGroups,omitempty"`
	// ActiveSpace is the Space the Workspace shows, "" for the default Space.
	ActiveSpace string `json:"activeSpace,omitempty"`
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
	History          []string `json:"history"`
	Keys             []string `json:"keys"`
	SelectedIdentity string   `json:"defaultKey"`
	Workspace        struct {
		ListWidth       int      `json:"listWidth"`
		CollapsedGroups []string `json:"collapsedGroups"`
	} `json:"workspace"`
}

func (s singleFileState) state() State {
	return State{History: s.History, Keys: s.Keys, SelectedIdentity: s.SelectedIdentity, CollapsedGroups: s.Workspace.CollapsedGroups, ListWidth: s.Workspace.ListWidth}
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

// GroupIndex returns the position in Groups of the Group named name in
// space ("" for the default Space), or -1.
func (c *Config) GroupIndex(space, name string) int {
	for i, g := range c.Groups {
		if g.Space == space && g.Name == name {
			return i
		}
	}
	return -1
}

// SetMetadata sets alias's Space ("" for the default Space), description
// and group, a Group of that Space. Metadata with none of them is dropped,
// so an emptied Connection leaves nothing behind.
func (c *Config) SetMetadata(alias, space, description, group string) {
	if space == "" && description == "" && group == "" {
		delete(c.Connections, alias)
		return
	}
	c.Connections[alias] = Connection{Alias: alias, Description: description, Group: group, Space: space}
	c.addMissingGroups()
}

// addMissingGroups gives every Group a Connection names but Groups lacks a
// record, alphabetically after the existing ones. It reports whether it
// added any.
func (c *Config) addMissingGroups() bool {
	var missing []Group
	for _, conn := range c.Connections {
		g := Group{Name: conn.Group, Space: conn.Space}
		if g.Name != "" && c.GroupIndex(g.Space, g.Name) < 0 && !slices.Contains(missing, g) {
			missing = append(missing, g)
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Space != missing[j].Space {
			return missing[i].Space < missing[j].Space
		}
		return missing[i].Name < missing[j].Name
	})
	c.Groups = append(c.Groups, missing...)
	return len(missing) > 0
}

// renameDigitGroups renames every Group made only of digits, in its record,
// its members and the collapsed list, and records a notice for each.
func (c *Config) renameDigitGroups() {
	used := map[Group]bool{}
	var digits []Group
	note := func(g Group) {
		if !used[g] {
			used[g] = true
			if allDigits(g.Name) {
				digits = append(digits, g)
			}
		}
	}
	for _, g := range c.Groups {
		note(Group{Name: g.Name, Space: g.Space})
	}
	for _, conn := range c.Connections {
		if conn.Group != "" {
			note(Group{Name: conn.Group, Space: conn.Space})
		}
	}
	sort.Slice(digits, func(i, j int) bool { return digits[i].Name < digits[j].Name })
	for _, old := range digits {
		name := "group-" + old.Name
		for n := 2; used[Group{Name: name, Space: old.Space}]; n++ {
			name = fmt.Sprintf("group-%s-%d", old.Name, n)
		}
		used[Group{Name: name, Space: old.Space}] = true
		c.RenameGroup(old.Space, old.Name, name)
		c.Notices = append(c.Notices, fmt.Sprintf("renamed group %q to %q: a group name may not be only digits", old.Name, name))
	}
}

// RenameGroup renames space's Group old to name in its record, its members
// and the collapsed list. It does not check name; see CheckGroupName.
func (c *Config) RenameGroup(space, old, name string) {
	if i := c.GroupIndex(space, old); i >= 0 {
		c.Groups[i].Name = name
	}
	for alias, conn := range c.Connections {
		if conn.Space == space && conn.Group == old {
			conn.Group = name
			c.Connections[alias] = conn
		}
	}
	c.renameCollapsed(CollapsedKey(space, old), CollapsedKey(space, name))
}

// DeleteGroup removes the record of space's Group name and leaves its
// members ungrouped; a member left with nothing else keeps no metadata.
func (c *Config) DeleteGroup(space, name string) {
	// Members go first: SetMetadata would give a Group still named by a
	// member its record back.
	for alias, conn := range c.Connections {
		if conn.Space == space && conn.Group == name {
			c.SetMetadata(alias, space, conn.Description, "")
		}
	}
	if i := c.GroupIndex(space, name); i >= 0 {
		c.Groups = slices.Delete(c.Groups, i, i+1)
	}
	c.renameCollapsed(CollapsedKey(space, name), "")
}

// CollapsedKey is how CollapsedGroups names space's Group group: the Group
// name in the default Space, "space/group" in any other.
func CollapsedKey(space, group string) string {
	if space == "" {
		return group
	}
	return space + "/" + group
}

// renameCollapsed keeps the per-machine collapsed list in step with a
// Group whose CollapsedKey changed from old to key, or that was deleted
// when key is "". It is the only code that follows a Group into that list.
func (c *Config) renameCollapsed(old, key string) {
	groups := c.CollapsedGroups
	if key == "" {
		c.CollapsedGroups = slices.DeleteFunc(groups, func(g string) bool { return g == old })
		return
	}
	for i, g := range groups {
		if g == old {
			groups[i] = key
		}
	}
}

// migrateGroups brings c up to Version: a default Space and a record for
// every Space and Group something names, and no all-digit Group names. It
// reports whether c changed.
func (c *Config) migrateGroups() bool {
	changed := c.Version < Version
	c.Version = Version
	n := len(c.Notices)
	if c.addMissingSpaces() {
		changed = true
	}
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

func New() *Config {
	return &Config{Version: Version, Spaces: []Space{{Name: DefaultSpaceName, Default: true}}, Connections: map[string]Connection{}}
}

// Paths returns the config path and the legacy files Load migrates from.
// The config lives in ~/.config/sshctl/ on every OS, or in
// $XDG_CONFIG_HOME/sshctl/ when that is set (ADR-0004).
func Paths() (string, []string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, err
	}
	dir, _ := configDirs(runtime.GOOS, home, os.Getenv)
	primary := filepath.Join(dir, "config.json")
	legacy := []string{filepath.Join(home, ".sshctl", "connections.json"), filepath.Join(home, ".sshyeah", "connections.json")}
	if runtime.GOOS == "windows" {
		legacy = append(legacy, filepath.Join(home, "sshctl", "connections.json"), filepath.Join(home, "sshyeah", "connections.json"))
	}
	return primary, legacy, nil
}

// configDirs returns sshctl's config directory and, on macOS and Windows,
// the old OS-specific one that MoveOld moves from ("" when there is none).
// A relative XDG_CONFIG_HOME is ignored, as the XDG spec says.
func configDirs(goos, home string, getenv func(string) string) (dir, old string) {
	base := getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".config")
	}
	switch goos {
	case "darwin":
		old = filepath.Join(home, "Library", "Application Support", "sshctl")
	case "windows":
		if appData := getenv("AppData"); appData != "" {
			old = filepath.Join(appData, "sshctl")
		}
	}
	return filepath.Join(base, "sshctl"), old
}

// MoveOld moves config.json and state.json from the old OS-specific config
// folder (~/Library/Application Support/sshctl on macOS, %AppData%\sshctl
// on Windows) to the folder of path, the config Paths returns. Call it
// before Load. It returns a one-line notice when it moved something, or a
// hint when both folders hold a config: the new one wins and the old one is
// left as it is. It returns "" when there is nothing to move.
func MoveOld(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	_, old := configDirs(runtime.GOOS, home, os.Getenv)
	return moveOld(old, path)
}

func moveOld(oldDir, path string) (string, error) {
	newDir := filepath.Dir(path)
	if oldDir == "" || filepath.Clean(oldDir) == filepath.Clean(newDir) {
		return "", nil
	}
	names := []string{"state.json", "config.json"} // config.json last: once it is moved, the move is done
	var found []string
	for _, name := range names {
		if exists(filepath.Join(oldDir, name)) {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	if exists(path) || exists(StatePath(path)) {
		return fmt.Sprintf("using the config in %s; the old one in %s is ignored and can be removed", newDir, oldDir), nil
	}
	if err := os.MkdirAll(newDir, 0700); err != nil {
		return "", err
	}
	for _, name := range found {
		if err := moveFile(filepath.Join(oldDir, name), filepath.Join(newDir, name)); err != nil {
			return "", fmt.Errorf("move config to %s: %w", newDir, err)
		}
	}
	_ = os.Remove(oldDir) // only succeeds when nothing else is left in it
	return fmt.Sprintf("moved the config from %s to %s", oldDir, newDir), nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// moveFile renames from to to, or copies and removes it when a rename
// cannot cross filesystems.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(to, b, 0600); err != nil {
		return err
	}
	return os.Remove(from)
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
	c, err := loadConfig(path, legacy, state, haveState)
	if err != nil {
		return nil, err
	}
	// A config synced from another machine may have renamed or deleted
	// the Space this machine had open.
	if c.ActiveSpace != "" && c.SpaceIndex(c.ActiveSpace) < 0 {
		c.ActiveSpace = ""
	}
	return c, nil
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
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(path, append(b, '\n'), 0600); err != nil {
		return err
	}
	b, err = json.MarshalIndent(stateFile{Version: StateVersion, State: c.State}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(StatePath(path), append(b, '\n'), 0600)
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
