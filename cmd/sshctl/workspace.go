package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/charmbracelet/x/term"
	"github.com/sshctl/sshctl/internal/config"
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
		Load:      a.workspaceSnapshot,
		PrefixKey: a.cfg.Workspace.PrefixKey,
		ListWidth: a.cfg.Workspace.ListWidth,
	})
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
	snap := workspace.Snapshot{Identity: cfg.DefaultKey}
	seen := map[string]bool{}
	for _, alias := range aliases {
		seen[alias] = true
		c := cfg.Connections[alias]
		snap.Entries = append(snap.Entries, workspace.Entry{Alias: alias, Description: c.Description, Group: c.Group})
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
