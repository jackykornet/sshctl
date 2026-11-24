package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/tunnel"
	"github.com/sshctl/sshctl/internal/workspace"
)

func TestSessionCommandRunsClaudeWithRemoteTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	c.SelectedIdentity = "/k/id"
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	a := &app{cfgPath: path, cfg: config.New()}
	tests := []struct {
		name   string
		claude bool
		want   []string
	}{
		{"plain tab", false, []string{"ssh", "-i", "/k/id", "web"}},
		{"claude tab", true, []string{"ssh", "-t", "-i", "/k/id", "web", "claude"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.sessionCommand("web", workspace.Launch{Claude: tt.claude})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sessionCommand(web, %v) = %v, want %v", tt.claude, got, tt.want)
			}
		})
	}
}

func TestSessionCommandRunsActionAsSshctlItself(t *testing.T) {
	a := &app{cfgPath: filepath.Join(t.TempDir(), "config.json"), cfg: config.New()}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []workspace.Action{workspace.ActionBootstrap, workspace.ActionProvision} {
		got, err := a.sessionCommand("web", workspace.Launch{Action: action})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{self, string(action), "--", "web"}; !slices.Equal(got, want) {
			t.Errorf("sessionCommand(web, %s) = %v, want %v", action, got, want)
		}
	}
}

func TestSessionCommandRunsTheConnectionsTunnelsInBatchMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	c.SelectedIdentity = "/k/id"
	c.SetTunnels("web", []tunnel.Tunnel{{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}})
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	a := &app{cfgPath: path, cfg: config.New()}
	got, err := a.sessionCommand("web", workspace.Launch{Tunnel: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-N", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3",
		"-o", "BatchMode=yes", "-i", "/k/id", "-L", "8083:localhost:8083", "web"}
	if !slices.Equal(got, want) {
		t.Errorf("sessionCommand(web, tunnel) = %v, want %v", got, want)
	}
	if _, err := a.sessionCommand("db", workspace.Launch{Tunnel: true}); err == nil {
		t.Error("a Connection with no Tunnels has nothing to run")
	}
}
