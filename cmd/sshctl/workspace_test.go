package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sshctl/sshctl/internal/catalog"
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

func TestWorkspaceSnapshotShowsConnectionsTheCatalogSaves(t *testing.T) {
	dir := t.TempDir()
	ssh := &openssh.Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
	if err := os.WriteFile(ssh.SSHConfig, []byte(openssh.IncludeLine+"\nHost mine\n  HostName example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sshctl.json")
	a := &app{cfgPath: path, cfg: config.New(), ssh: ssh}
	cat := catalog.New(path, nil, ssh)
	if err := cat.Save("", workspace.Spec{Alias: "api", Group: "prod", Managed: true, Host: workspace.Host{HostName: "10.0.0.9", Port: "2222"}}); err != nil {
		t.Fatal(err)
	}
	if err := cat.Save("mine", workspace.Spec{Alias: "mine", Description: "laptop"}); err != nil {
		t.Fatal(err)
	}
	snap, err := a.workspaceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := []workspace.Entry{
		{Alias: "api", Group: "prod", Managed: true, Host: workspace.Host{HostName: "10.0.0.9", Port: "2222"}},
		{Alias: "mine", Description: "laptop"},
	}
	if !slices.Equal(snap.Entries, want) {
		t.Fatalf("entries = %+v, want %+v", snap.Entries, want)
	}
}
