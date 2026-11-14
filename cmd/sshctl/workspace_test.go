package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/workspace"
)

func TestSessionCommandRunsClaudeWithRemoteTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	c.DefaultKey = "/k/id"
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
			got, err := a.sessionCommand("web", tt.claude)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sessionCommand(web, %v) = %v, want %v", tt.claude, got, tt.want)
			}
		})
	}
}

func TestWorkspaceSavesGroupsAndCollapsedGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	c.Connections["web"] = config.Connection{Alias: "web", Description: "site", Group: "prod"}
	c.Connections["tmp"] = config.Connection{Alias: "tmp", Group: "scratch"}
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	a := &app{cfgPath: path, cfg: config.New()}
	for _, mv := range [][2]string{{"web", "staging"}, {"db", "data"}, {"tmp", ""}} {
		if err := a.setGroup(mv[0], mv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.saveCollapsed([]string{"data", "staging"}); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if web := got.Connections["web"]; web.Group != "staging" || web.Description != "site" {
		t.Errorf("web = %+v, want group staging keeping its description", web)
	}
	if db := got.Connections["db"]; db.Alias != "db" || db.Group != "data" {
		t.Errorf("db = %+v, want new metadata in group data", db)
	}
	if _, ok := got.Connections["tmp"]; ok {
		t.Error("tmp has no description or group left and should lose its metadata")
	}
	if want := []string{"data", "staging"}; !slices.Equal(got.Workspace.CollapsedGroups, want) {
		t.Errorf("collapsed = %v, want %v", got.Workspace.CollapsedGroups, want)
	}
}

func TestSaveAndDeleteConnectionWriteManagedHostsAndMetadata(t *testing.T) {
	dir := t.TempDir()
	ssh := &openssh.Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
	if err := os.WriteFile(ssh.SSHConfig, []byte("Host mine\n  HostName example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sshctl.json")
	c := config.New()
	c.History = []string{"api"}
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	a := &app{cfgPath: path, cfg: config.New(), ssh: ssh}
	api := workspace.Spec{Alias: "api", Group: "prod", Managed: true, Host: workspace.Host{HostName: "10.0.0.9", Port: "2222"}}

	var inc *workspace.IncludeError
	if err := a.saveConnection("", api); !errors.As(err, &inc) {
		t.Fatalf("err = %v, want an IncludeError before ssh_config Includes sshctl's file", err)
	}
	if hosts, _ := ssh.ManagedHosts(); len(hosts) != 0 {
		t.Fatal("nothing should be written before the Include exists")
	}
	if err := ssh.AddInclude(); err != nil {
		t.Fatal(err)
	}
	if err := a.saveConnection("", api); err != nil {
		t.Fatal(err)
	}
	if err := a.saveConnection("", workspace.Spec{Alias: "mine", Managed: true, Host: workspace.Host{HostName: "x"}}); err == nil {
		t.Error("adding an alias that ssh_config already has should fail")
	}
	snap, err := a.workspaceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	var got workspace.Entry
	for _, e := range snap.Entries {
		if e.Alias == "api" {
			got = e
		}
	}
	if !got.Managed || got.Group != "prod" || got.Host.Port != "2222" {
		t.Fatalf("api entry = %+v", got)
	}

	api.Alias = "api2"
	if err := a.saveConnection("api", api); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path, nil)
	if _, ok := cfg.Connections["api"]; ok || cfg.Connections["api2"].Group != "prod" || cfg.History[0] != "api2" {
		t.Fatalf("metadata after rename = %+v history = %v", cfg.Connections, cfg.History)
	}
	if err := a.saveConnection("mine", workspace.Spec{Alias: "mine2", Description: "x"}); err == nil {
		t.Error("renaming a Host block sshctl does not manage should fail")
	}

	if err := a.deleteConnection("api2"); err != nil {
		t.Fatal(err)
	}
	if hosts, _ := ssh.ManagedHosts(); len(hosts) != 0 {
		t.Fatalf("hosts = %v, want none", hosts)
	}
	cfg, _ = config.Load(path, nil)
	if _, ok := cfg.Connections["api2"]; ok {
		t.Error("metadata should be deleted with the host")
	}
	if b, _ := os.ReadFile(ssh.SSHConfig); !strings.Contains(string(b), "Host mine") {
		t.Error("the user's own Host block must be untouched")
	}
}
