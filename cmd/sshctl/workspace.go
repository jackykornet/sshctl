package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/platform"
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
		ReadClipboard:   platform.ReadClipboard,
		Resolve:         resolveEndpoint,
	})
}

// resolveTimeout bounds the "ssh -G" behind the Workspace pane's header.
const resolveTimeout = 5 * time.Second

// resolveEndpoint is the user, host and port ssh uses for alias, for the
// Workspace pane's header.
func resolveEndpoint(alias string) (openssh.Endpoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	// Not openssh.Run: the Workspace owns the terminal, so ssh gets neither
	// its input nor its error output.
	quiet := func(ctx context.Context, argv []string, out io.Writer) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdout = out
		return cmd.Run()
	}
	return openssh.Resolve(ctx, quiet, alias)
}

// sessionCommand builds the command line for a new tab. An Action runs this
// sshctl binary's own command for alias ("sshctl bootstrap -- <alias>"), which
// reads the config itself. Otherwise it builds ssh using alias's Selected
// identity (its Space's own, or else the global one) as it is now, so a
// change made with "sshctl key use" applies to the next tab. LaunchTunnel runs alias's Tunnels as the
// config holds them now, with BatchMode, since it has no terminal to prompt in.
func (a *app) sessionCommand(alias string, l workspace.Launch) ([]string, error) {
	if l.Action != "" {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return []string{self, string(l.Action), "--", alias}, nil
	}
	cfg, err := config.Load(a.cfgPath, a.legacy)
	if err != nil {
		return nil, err
	}
	a.cfg = cfg
	conn := cfg.Connections[alias]
	if l.Kind == workspace.LaunchTunnel {
		if len(conn.Tunnels) == 0 {
			return nil, fmt.Errorf("%s has no Tunnels", alias)
		}
		return openssh.TunnelArgs(alias, cfg.IdentityFor(conn.Space), conn.Tunnels, true), nil
	}
	return openssh.ConnectArgs(alias, cfg.IdentityFor(conn.Space)), nil
}
