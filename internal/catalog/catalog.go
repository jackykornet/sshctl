// Package catalog is the Connection catalog: the one place that reads and
// changes Connections across sshctl's config (description, group, History)
// and ssh_config (Managed connections' Host blocks, ADR-0002).
package catalog

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// Host is how OpenSSH reaches a Managed connection.
type Host = openssh.Endpoint

// Group is a stored Group record (ADR-0003). A Group's number is its
// position among its Space's Groups in Snapshot.Groups, counting from 1.
type Group = config.Group

// Space is a stored Space record (ADR-0005). Its Number is its fixed Space
// number (ADR-0010); Snapshot.Spaces lists Spaces in number order.
type Space = config.Space

// MaxSpaces is how many Space numbers there are, and so how many Spaces
// (ADR-0010).
const MaxSpaces = config.MaxSpaces

// ErrUnknownGroup is returned when a Connection is moved into a Group that
// has no record.
var ErrUnknownGroup = errors.New("no such group")

// Spec is a Connection as it is to be saved.
type Spec struct {
	Alias       string
	Description string
	// Space is the Space's name, or "" for the default Space. Group is a
	// Group of that Space.
	Space string
	Group string
	// Managed is true when sshctl writes the Host block, so Host applies.
	// For a Connection defined elsewhere in ssh_config only the description
	// and group are saved.
	Managed bool
	Host    Host
}

// Entry is one Connection as listed from ssh_config and sshctl metadata.
type Entry struct {
	Alias       string
	Description string
	// Space is the Space the Connection belongs to, "" for the default
	// Space, as Group.Space and Snapshot.ActiveSpace name it.
	Space string
	Group string
	// Orphaned is true when sshctl metadata exists but the Host alias is gone.
	Orphaned bool
	// Managed is true when the Host block is in sshctl's own ssh_config file,
	// so Host applies and the block can be edited and deleted.
	Managed bool
	Host    Host
}

// Experimental says which Experimental features are on; see
// config.Experimental.
type Experimental = config.Experimental

// The Experimental features, by name.
const (
	FeatureBootstrap = config.FeatureBootstrap
	FeatureProvision = config.FeatureProvision
)

// CollapsedKey is how Snapshot.Collapsed and SetCollapsed name space's
// Group group: the Group name in the default Space (""), "space/group" in
// any other.
func CollapsedKey(space, group string) string { return config.CollapsedKey(space, group) }

// Snapshot is the Connection list as it is on disk now.
type Snapshot struct {
	// Entries lists ssh_config's aliases in ssh_config order, then the
	// Orphaned connections sorted by alias.
	Entries []Entry
	// Spaces lists every Space in the user's order.
	Spaces []Space
	// ActiveSpace is the Space the Workspace shows on this machine, ""
	// for the default Space.
	ActiveSpace string
	// Groups lists every Group of every Space in the user's order,
	// including empty ones.
	Groups []Group
	// Identity is the global Selected identity's key path, or "" when
	// OpenSSH chooses keys.
	Identity string
	// SpaceIdentities is the Selected identity of each Space that selects
	// its own, by how Connections refer to the Space; see SpaceIdentity.
	SpaceIdentities map[string]string
	// Collapsed lists the groups the Workspace shows collapsed on this
	// machine, by CollapsedKey (ADR-0004).
	Collapsed []string
	// Experimental says which Experimental features are on.
	Experimental Experimental
	// Include is set when sshctl's hosts file exists but the user's
	// ssh_config does not Include it, so ssh cannot see its Host blocks, as
	// on a new machine the file was synced to (ADR-0004). AddInclude fixes it.
	Include *IncludeError
}

// SpaceIdentity returns the Selected identity for the Connections of the
// Space ref ("" for the default Space): its own, or else the global one.
func (s Snapshot) SpaceIdentity(ref string) string {
	return (&config.Config{State: config.State{SelectedIdentity: s.Identity, SpaceIdentities: s.SpaceIdentities}}).IdentityFor(ref)
}

// Space returns the Space named name ("" also names the default Space),
// and whether there is one.
func (s Snapshot) Space(name string) (Space, bool) {
	ref, ok := s.SpaceRef(name)
	if !ok {
		return Space{}, false
	}
	return s.Spaces[(&config.Config{Spaces: s.Spaces}).SpaceIndex(ref)], true
}

// SpaceNumbered returns the Space with Space number n, and whether one has
// it; no Space has a number outside 1..MaxSpaces.
func (s Snapshot) SpaceNumbered(n int) (Space, bool) {
	if n < 1 || n > MaxSpaces {
		return Space{}, false
	}
	if i := (&config.Config{Spaces: s.Spaces}).SpaceNumbered(n); i >= 0 {
		return s.Spaces[i], true
	}
	return Space{}, false
}

// FreeSpaceNumber returns the lowest Space number no Space has, or 0 when
// all MaxSpaces are taken.
func (s Snapshot) FreeSpaceNumber() int {
	return (&config.Config{Spaces: s.Spaces}).FreeSpaceNumber()
}

// SpaceName returns the name of the Space ref ("" for the default Space).
func (s Snapshot) SpaceName(ref string) string {
	if i := (&config.Config{Spaces: s.Spaces}).SpaceIndex(ref); i >= 0 {
		return s.Spaces[i].Name
	}
	return ""
}

// Entry returns the Connection alias, Orphaned or not, and whether there
// is one.
func (s Snapshot) Entry(alias string) (Entry, bool) {
	for _, e := range s.Entries {
		if e.Alias == alias {
			return e, true
		}
	}
	return Entry{}, false
}

// HasGroup reports whether space ("" for the default Space) has a Group
// named name.
func (s Snapshot) HasGroup(space, name string) bool {
	return slices.ContainsFunc(s.Groups, func(g Group) bool { return g.Space == space && g.Name == name })
}

// SpaceRef returns how Groups and Connections refer to the Space named
// name ("" for the default Space, which "" also names), and whether there
// is one.
func (s Snapshot) SpaceRef(name string) (string, bool) {
	return (&config.Config{Spaces: s.Spaces}).SpaceRef(name)
}

// GroupNames returns the names of space's Groups ("" for the default
// Space) in order, so the Group number n names GroupNames(space)[n-1].
func (s Snapshot) GroupNames(space string) []string {
	var names []string
	for _, g := range s.Groups {
		if g.Space == space {
			names = append(names, g.Name)
		}
	}
	return names
}

// IncludeError is returned by Save when the user's ssh_config does not yet
// read sshctl's hosts file. Saving can be retried after AddInclude adds Line
// to Config.
type IncludeError struct {
	Line   string
	Config string
}

func (e *IncludeError) Error() string { return openssh.MissingInclude(e.Config, e.Line) }

// Catalog reads and writes Connections in sshctl's config file and
// ssh_config.
type Catalog struct {
	configPath string
	legacy     []string
	ssh        *openssh.Client
	// saveConfig writes the sshctl config and saveHostBlock a Host block in
	// sshctl's ssh_config file; tests make them fail.
	saveConfig    func(string, *config.Config) error
	saveHostBlock func(old string, h openssh.Host) error
}

// New returns the Connection catalog over the sshctl config at configPath
// (migrated from legacy when missing) and ssh.
func New(configPath string, legacy []string, ssh *openssh.Client) *Catalog {
	return &Catalog{configPath: configPath, legacy: legacy, ssh: ssh, saveConfig: config.Save, saveHostBlock: ssh.SaveHost}
}

// Snapshot reads ssh_config's aliases and sshctl metadata fresh from disk,
// so a refresh picks up changes made in another terminal.
func (c *Catalog) Snapshot() (Snapshot, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return Snapshot{}, err
	}
	aliases, err := c.ssh.Aliases()
	if err != nil {
		return Snapshot{}, err
	}
	hosts, err := c.ssh.ManagedHosts()
	if err != nil {
		return Snapshot{}, err
	}
	managed := make(map[string]openssh.Host, len(hosts))
	for _, h := range hosts {
		managed[h.Alias] = h
	}
	snap := Snapshot{Spaces: cfg.Spaces, ActiveSpace: cfg.ActiveSpace, Groups: cfg.Groups, Identity: cfg.SelectedIdentity, SpaceIdentities: cfg.SpaceIdentities, Collapsed: cfg.CollapsedGroups, Experimental: cfg.Experimental}
	if snap.Include, err = c.missingInclude(); err != nil {
		return Snapshot{}, err
	}
	seen := map[string]bool{}
	for _, alias := range aliases {
		seen[alias] = true
		m := cfg.Connections[alias]
		e := Entry{Alias: alias, Description: m.Description, Space: m.Space, Group: m.Group}
		if h, ok := managed[alias]; ok {
			e.Managed = true
			e.Host = h.Endpoint
		}
		snap.Entries = append(snap.Entries, e)
	}
	var orphans []string
	for alias := range cfg.Connections {
		if !seen[alias] {
			orphans = append(orphans, alias)
		}
	}
	sort.Strings(orphans)
	for _, alias := range orphans {
		m := cfg.Connections[alias]
		snap.Entries = append(snap.Entries, Entry{Alias: alias, Description: m.Description, Space: m.Space, Group: m.Group, Orphaned: true})
	}
	return snap, nil
}

// missingInclude returns the Include the user's ssh_config lacks when
// sshctl's hosts file exists, and nil when there is no such file or it is
// already Included. A symlink counts as the file even when its target is
// missing, as when a dotfiles manager has not applied it yet.
func (c *Catalog) missingInclude() (*IncludeError, error) {
	if _, err := os.Lstat(c.ssh.ManagedPath()); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	included, err := c.ssh.IncludesManaged()
	if err != nil || included {
		return nil, err
	}
	return c.includeError(), nil
}

func (c *Catalog) includeError() *IncludeError {
	home, _ := os.UserHomeDir()
	return &IncludeError{Line: openssh.IncludeLine, Config: displayPath(c.ssh.SSHConfig, home)}
}

// History returns the aliases opened, most recent first, as the config
// holds them now.
func (c *Catalog) History() ([]string, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return nil, err
	}
	return cfg.History, nil
}

// RecordOpen records in History that alias was opened.
func (c *Catalog) RecordOpen(alias string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.RecordOpen(alias)
		return nil
	})
}

// SetGroup puts alias in group ("" for none), a Group of alias's Space,
// keeping its description. The Group must already have a record;
// otherwise SetGroup returns ErrUnknownGroup and changes nothing.
func (c *Catalog) SetGroup(alias, group string) error {
	return c.update(func(cfg *config.Config) error {
		m := cfg.Connections[alias]
		if group != "" {
			if err := knownGroup(cfg, m.Space, group); err != nil {
				return err
			}
		}
		cfg.SetMetadata(alias, m.Space, m.Description, group)
		return nil
	})
}

// SetCollapsed saves groups as the groups the Workspace shows collapsed.
func (c *Catalog) SetCollapsed(groups []string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.CollapsedGroups = groups
		return nil
	})
}

// SetIdentity makes key (a private key path) the Selected identity; ""
// leaves the choice of keys to OpenSSH.
func (c *Catalog) SetIdentity(key string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.SelectedIdentity = key
		return nil
	})
}

// IdentityFor returns the Selected identity ssh uses for alias: its
// Space's own, or else the global one; "" when OpenSSH chooses keys.
func (c *Catalog) IdentityFor(alias string) (string, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return "", err
	}
	return cfg.IdentityFor(cfg.Connections[alias].Space), nil
}

// SetSpaceIdentity makes key (a private key path) the Selected identity of
// the Space space's Connections; "" unsets it, so the global one applies.
func (c *Catalog) SetSpaceIdentity(space, key string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if key == "" {
			delete(cfg.SpaceIdentities, ref)
			return nil
		}
		if cfg.SpaceIdentities == nil {
			cfg.SpaceIdentities = map[string]string{}
		}
		cfg.SpaceIdentities[ref] = key
		return nil
	})
}

// CheckFeature returns nil when the Experimental feature is on, and
// otherwise an error wrapping config.ErrFeatureOff that says how to turn
// it on.
func (c *Catalog) CheckFeature(feature string) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	return cfg.Experimental.Check(feature)
}

// SetFeature turns an Experimental feature on or off. It refuses one
// config.Features does not list, with config.ErrUnknownFeature.
func (c *Catalog) SetFeature(feature string, on bool) error {
	return c.update(func(cfg *config.Config) error { return cfg.Experimental.Set(feature, on) })
}

// Dotfiles returns the global dotfiles setting: a repo URL,
// config.DotfilesNone, or "" when unset.
func (c *Catalog) Dotfiles() (string, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return "", err
	}
	return cfg.Provision.Dotfiles, nil
}

// SetDotfiles saves the global dotfiles setting ("" unsets it).
func (c *Catalog) SetDotfiles(repo string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.Provision.Dotfiles = repo
		return nil
	})
}

// SetGroupDotfiles saves the dotfiles setting on the record of space's
// Group group ("" unsets it, so the global setting applies). The Group must
// already have a record; otherwise SetGroupDotfiles returns ErrUnknownGroup
// and changes nothing.
func (c *Catalog) SetGroupDotfiles(space, group, repo string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if err := knownGroup(cfg, ref, group); err != nil {
			return err
		}
		cfg.Groups[cfg.GroupIndex(ref, group)].Dotfiles = repo
		return nil
	})
}

var (
	// ErrNoDotfiles is returned by ProvisionRepo when no dotfiles repo is set.
	ErrNoDotfiles = errors.New("no dotfiles repo is set")
	// ErrProvisionRefused is returned by ProvisionRepo when the setting
	// that applies is config.DotfilesNone.
	ErrProvisionRefused = errors.New("Provision is refused by the dotfiles setting none")
)

// ProvisionRepo returns the dotfiles repo Provision applies to alias's host,
// resolved Group → Space → global (ADR-0005): the first of its Group's, its
// Space's and the global setting that is set. There is no per-Connection
// setting. A setting of config.DotfilesNone refuses Provision whatever the
// settings after it are, and the error names the Group or Space it is on.
func (c *Catalog) ProvisionRepo(alias string) (string, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return "", err
	}
	m := cfg.Connections[alias]
	type setting struct{ repo, on string }
	var settings []setting
	if i := cfg.GroupIndex(m.Space, m.Group); m.Group != "" && i >= 0 {
		settings = append(settings, setting{cfg.Groups[i].Dotfiles, " of group " + m.Group})
	}
	if i := cfg.SpaceIndex(m.Space); i >= 0 {
		settings = append(settings, setting{cfg.Spaces[i].Dotfiles, " of space " + cfg.Spaces[i].Name})
	}
	settings = append(settings, setting{cfg.Provision.Dotfiles, ""})
	for _, s := range settings {
		switch s.repo {
		case "":
		case config.DotfilesNone:
			return "", fmt.Errorf("%w%s", ErrProvisionRefused, s.on)
		default:
			return s.repo, nil
		}
	}
	return "", ErrNoDotfiles
}

// update loads the sshctl config, applies change and saves it, unless
// change fails.
func (c *Catalog) update(change func(*config.Config) error) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	if err := change(cfg); err != nil {
		return err
	}
	return c.saveConfig(c.configPath, cfg)
}

// Save adds a Connection (old is "") or saves changes to old.
//
// A Host block is written to sshctl's ssh_config file whenever s is Managed
// or the alias is new or renamed; openssh.SaveHost enforces ADR-0002, and
// a Connection whose Host block is the user's cannot be renamed. Then the
// description and group are saved under the new alias, and History follows
// a rename.
//
// Nothing is changed when Save fails: the sshctl config is read before the
// Host block is written, and the Host block is put back when the config
// cannot be written. Save returns an *IncludeError when ssh_config must
// first Include sshctl's file, and refuses an unknown Space
// (ErrUnknownSpace), a Group name config.CheckGroupName refuses and, with
// ErrUnknownGroup, a Group the Space has no record of (AddGroup creates
// one).
func (c *Catalog) Save(old string, s Spec) error {
	if s.Group != "" {
		if err := config.CheckGroupName(s.Group); err != nil {
			return err
		}
	}
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	space, err := spaceRef(cfg, s.Space)
	if err != nil {
		return err
	}
	if s.Group != "" {
		if err := knownGroup(cfg, space, s.Group); err != nil {
			return err
		}
	}
	undo := func() error { return nil }
	if s.Managed || old != s.Alias {
		if undo, err = c.saveHost(old, s); err != nil {
			return err
		}
	}
	if old != "" && old != s.Alias {
		rename(cfg, old, s.Alias)
	}
	cfg.SetMetadata(s.Alias, space, s.Description, s.Group)
	if err := c.saveConfig(c.configPath, cfg); err != nil {
		if uerr := undo(); uerr != nil {
			return fmt.Errorf("%w (and restoring the Host block failed: %v)", err, uerr)
		}
		return err
	}
	return nil
}

// saveHost writes s's Host block in place of old's and returns how to put
// back what was there before.
func (c *Catalog) saveHost(old string, s Spec) (undo func() error, err error) {
	var prev openssh.Host
	if old != "" {
		hosts, err := c.ssh.ManagedHosts()
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			if h.Alias == old {
				prev = h
			}
		}
	}
	h := openssh.Host{Alias: s.Alias, Endpoint: s.Host}
	switch err := c.saveHostBlock(old, h); {
	case errors.Is(err, openssh.ErrNotIncluded):
		return nil, c.includeError()
	case errors.Is(err, openssh.ErrUnknownHost):
		return nil, c.notManaged(old, s.Alias)
	case err != nil:
		return nil, err
	}
	if old == "" {
		return func() error { return c.ssh.DeleteHost(s.Alias) }, nil
	}
	return func() error { return c.ssh.SaveHost(s.Alias, prev) }, nil
}

// notManaged explains why old's Host block, which is not in sshctl's file,
// cannot be saved as alias's.
func (c *Catalog) notManaged(old, alias string) error {
	aliases, err := c.ssh.Aliases()
	switch {
	case err != nil:
		return err
	case !slices.Contains(aliases, old):
		return fmt.Errorf("%s has no Host block in ssh_config to rename", old)
	case old != alias:
		return fmt.Errorf("%s is defined outside sshctl and cannot be renamed here", old)
	}
	return fmt.Errorf("%s's Host block is defined outside sshctl; only its description and group can be edited here", old)
}

// Delete removes alias's Host block when sshctl manages it, and its sshctl
// metadata. openssh.DeleteHost decides from ssh_config whether the block is
// sshctl's; a block anywhere else is left alone.
func (c *Catalog) Delete(alias string) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	if err := c.ssh.DeleteHost(alias); err != nil && !errors.Is(err, openssh.ErrUnknownHost) {
		return err
	}
	delete(cfg.Connections, alias)
	return c.saveConfig(c.configPath, cfg)
}

// ErrNoMetadata is returned by DeleteMetadata when alias has no sshctl metadata.
var ErrNoMetadata = errors.New("no sshctl metadata")

// DeleteMetadata removes alias's description, group and Space, which
// leaves it in the default Space, and nothing else: its Host block, even
// one sshctl manages, and its History stay.
func (c *Catalog) DeleteMetadata(alias string) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	if _, ok := cfg.Connections[alias]; !ok {
		return fmt.Errorf("%s: %w", alias, ErrNoMetadata)
	}
	delete(cfg.Connections, alias)
	return c.saveConfig(c.configPath, cfg)
}

// rename is the rename rule: old's metadata and History follow it to alias.
func rename(cfg *config.Config, old, alias string) {
	if m, ok := cfg.Connections[old]; ok {
		delete(cfg.Connections, old)
		m.Alias = alias
		cfg.Connections[alias] = m
	}
	for i, h := range cfg.History {
		if h == old {
			cfg.History[i] = alias
		}
	}
}

// AddInclude adds the line an *IncludeError names to the top of the user's
// ssh_config, so OpenSSH reads sshctl's Host blocks.
func (c *Catalog) AddInclude() error { return c.ssh.AddInclude() }

// displayPath shows path under home as "~/...", with forward slashes on
// every platform, as ssh_config writes it.
func displayPath(path, home string) string { return openssh.HomeRelative(path, home) }
