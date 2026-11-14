package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		Catalog:         catalog.New(a.cfgPath, a.legacy, a.ssh),
		Command:         a.sessionCommand,
		PrefixKey:       a.cfg.Workspace.PrefixKey,
		ListWidth:       a.cfg.ListWidth,
		ScrollbackLines: a.cfg.Workspace.ScrollbackLines,
		Notice:          strings.Join(a.cfg.Notices, "; "),
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
