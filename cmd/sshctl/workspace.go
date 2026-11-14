package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/charmbracelet/x/term"
	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/workspace"
)

// interactive reports whether sshctl is attached to a terminal on both ends,
// which the Workspace needs. Otherwise sshctl without arguments prints help.
func interactive(in, out any) bool {
	fi, ok1 := in.(*os.File)
	fo, ok2 := out.(*os.File)
	return ok1 && ok2 && term.IsTerminal(fi.Fd()) && term.IsTerminal(fo.Fd())
}

func (a *app) runWorkspace() error {
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("the Workspace needs the OpenSSH client: %w", err)
	}
	return workspace.Run(workspace.Options{
		Load:            a.workspaceSnapshot,
		Command:         a.sessionCommand,
		OnOpen:          a.recordOpen,
		OnGroup:         a.setGroup,
		Collapsed:       a.cfg.Workspace.CollapsedGroups,
		OnCollapse:      a.saveCollapsed,
		Catalog:         catalog.New(a.cfgPath, a.legacy, a.ssh),
		PrefixKey:       a.cfg.Workspace.PrefixKey,
		ListWidth:       a.cfg.Workspace.ListWidth,
		ScrollbackLines: a.cfg.Workspace.ScrollbackLines,
	})
}

// sessionCommand builds ssh for a new tab using the Selected identity as it
// is now, so a change made with "sshctl key use" applies to the next tab.
// claude runs claude on the remote host.
func (a *app) sessionCommand(alias string, claude bool) ([]string, error) {
	cfg, err := config.Load(a.cfgPath, a.legacy)
	if err != nil {
		return nil, err
	}
	a.cfg = cfg
	return openssh.ConnectArgs(alias, cfg.DefaultKey, claude), nil
}

// recordOpen adds a Connection to History when it becomes open in the Workspace.
func (a *app) recordOpen(alias string) error {
	cfg, err := config.Load(a.cfgPath, a.legacy)
	if err != nil {
		return err
	}
	a.cfg = cfg
	return a.recordHistory(alias)
}

// setGroup saves alias's group ("" for none) in sshctl metadata, dropping the
// metadata entry when nothing is left in it.
func (a *app) setGroup(alias, group string) error {
	cfg, err := config.Load(a.cfgPath, a.legacy)
	if err != nil {
		return err
	}
	a.cfg = cfg
	cfg.SetMetadata(alias, cfg.Connections[alias].Description, group)
	return a.save()
}

// managedHosts returns the Host blocks in sshctl's own ssh_config file by alias.
func (a *app) managedHosts() (map[string]openssh.Host, error) {
	hosts, err := a.ssh.ManagedHosts()
	if err != nil {
		return nil, err
	}
	out := make(map[string]openssh.Host, len(hosts))
	for _, h := range hosts {
		out[h.Alias] = h
	}
	return out, nil
}

// saveCollapsed saves which groups the Workspace connection list shows collapsed.
func (a *app) saveCollapsed(groups []string) error {
	cfg, err := config.Load(a.cfgPath, a.legacy)
	if err != nil {
		return err
	}
	a.cfg = cfg
	cfg.Workspace.CollapsedGroups = groups
	return a.save()
}

// workspaceSnapshot reads ssh_config aliases and sshctl metadata fresh from disk,
// so a refresh picks up changes made in another terminal.
func (a *app) workspaceSnapshot() (workspace.Snapshot, error) {
	cfg, err := config.Load(a.cfgPath, a.legacy)
	if err != nil {
		return workspace.Snapshot{}, err
	}
	a.cfg = cfg
	aliases, err := a.ssh.Aliases()
	if err != nil {
		return workspace.Snapshot{}, err
	}
	managed, err := a.managedHosts()
	if err != nil {
		return workspace.Snapshot{}, err
	}
	snap := workspace.Snapshot{Identity: cfg.DefaultKey}
	seen := map[string]bool{}
	for _, alias := range aliases {
		seen[alias] = true
		c := cfg.Connections[alias]
		e := workspace.Entry{Alias: alias, Description: c.Description, Group: c.Group}
		if h, ok := managed[alias]; ok {
			e.Managed = true
			e.Host = workspace.Host{HostName: h.HostName, User: h.User, Port: h.Port, IdentityFile: h.IdentityFile}
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
		c := cfg.Connections[alias]
		snap.Entries = append(snap.Entries, workspace.Entry{Alias: alias, Description: c.Description, Group: c.Group, Orphaned: true})
	}
	return snap, nil
}
