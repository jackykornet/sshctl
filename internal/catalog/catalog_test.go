package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// tree is a temporary sshctl config and ssh_config. The user's ssh_config
// holds userConfig and, when included is set, Includes sshctl's file first.
type tree struct {
	cat *Catalog
	ssh *openssh.Client
	cfg string
}

func newTree(t *testing.T, included bool, userConfig string) *tree {
	t.Helper()
	dir := t.TempDir()
	ssh := &openssh.Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
	if included {
		userConfig = openssh.IncludeLine + "\n" + userConfig
	}
	if err := os.WriteFile(ssh.SSHConfig, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "sshctl.json")
	return &tree{cat: New(cfg, nil, ssh), ssh: ssh, cfg: cfg}
}

func (tr *tree) config(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Load(tr.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (tr *tree) hosts(t *testing.T) map[string]openssh.Host {
	t.Helper()
	hosts, err := tr.ssh.ManagedHosts()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]openssh.Host{}
	for _, h := range hosts {
		out[h.Alias] = h
	}
	return out
}

func TestSaveAddsManagedConnection(t *testing.T) {
	tr := newTree(t, true, "")
	err := tr.cat.Save("", Spec{Alias: "api", Group: "prod", Managed: true, Host: Host{HostName: "10.0.0.9", Port: "2222"}})
	if err != nil {
		t.Fatal(err)
	}
	if h := tr.hosts(t)["api"]; h.HostName != "10.0.0.9" || h.Port != "2222" {
		t.Errorf("api Host block = %+v", h)
	}
	if c := tr.config(t).Connections["api"]; c.Group != "prod" {
		t.Errorf("api metadata = %+v, want group prod", c)
	}
}

func TestSaveAsksForIncludeBeforeWritingAnything(t *testing.T) {
	tr := newTree(t, false, "Host mine\n  HostName example.com\n")
	err := tr.cat.Save("", Spec{Alias: "api", Group: "prod", Managed: true, Host: Host{HostName: "h"}})
	var inc *IncludeError
	// Config may be shown from "~" when the temporary directory is under home.
	if !errors.As(err, &inc) || inc.Line != "Include sshctl_config" || filepath.Base(inc.Config) != "config" {
		t.Fatalf("err = %v, want an IncludeError naming the line and the user's ssh_config", err)
	}
	if len(tr.hosts(t)) != 0 || len(tr.config(t).Connections) != 0 {
		t.Fatal("nothing should be written before the Include exists")
	}
	if err := tr.cat.AddInclude(); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("", Spec{Alias: "api", Managed: true, Host: Host{HostName: "h"}}); err != nil {
		t.Fatalf("saving after AddInclude: %v", err)
	}
}

// seed saves cfg as the sshctl config.
func (tr *tree) seed(t *testing.T, cfg *config.Config) {
	t.Helper()
	if err := config.Save(tr.cfg, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRenamesManagedConnectionWithItsMetadataAndHistory(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.Save("", Spec{Alias: "api", Description: "API", Group: "prod", Managed: true, Host: Host{HostName: "h", User: "deploy"}}); err != nil {
		t.Fatal(err)
	}
	cfg := tr.config(t)
	cfg.History = []string{"db", "api"}
	tr.seed(t, cfg)

	if err := tr.cat.Save("api", Spec{Alias: "api2", Description: "API", Group: "prod", Managed: true, Host: Host{HostName: "h2", User: "deploy"}}); err != nil {
		t.Fatal(err)
	}
	hosts := tr.hosts(t)
	if _, ok := hosts["api"]; ok || hosts["api2"].HostName != "h2" {
		t.Errorf("Host blocks after rename = %+v", hosts)
	}
	got := tr.config(t)
	if _, ok := got.Connections["api"]; ok {
		t.Error("metadata should leave the old alias")
	}
	if c := got.Connections["api2"]; c.Alias != "api2" || c.Description != "API" || c.Group != "prod" {
		t.Errorf("api2 metadata = %+v", c)
	}
	if want := []string{"db", "api2"}; !slices.Equal(got.History, want) {
		t.Errorf("History = %v, want %v", got.History, want)
	}
}

func TestSaveEditsUnmanagedConnectionsMetadataOnly(t *testing.T) {
	const userConfig = "Host mine\n  HostName example.com\n"
	tr := newTree(t, false, userConfig) // no Include needed: ssh_config is not written
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop", Group: "home"}); err != nil {
		t.Fatal(err)
	}
	if c := tr.config(t).Connections["mine"]; c.Alias != "mine" || c.Description != "laptop" || c.Group != "home" {
		t.Errorf("mine metadata = %+v", c)
	}
	if err := tr.cat.Save("mine", Spec{Alias: "mine"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.config(t).Connections["mine"]; ok {
		t.Error("metadata with no description and no group should be dropped")
	}
	if b, _ := os.ReadFile(tr.ssh.SSHConfig); string(b) != userConfig {
		t.Errorf("the user's ssh_config changed:\n%s", b)
	}
	if len(tr.hosts(t)) != 0 {
		t.Error("no Host block should be written for the user's Connection")
	}
}

func TestSaveRefusesToRenameOrWriteTheUsersHostBlock(t *testing.T) {
	tr := newTree(t, true, "Host mine\n  HostName example.com\n")
	err := tr.cat.Save("mine", Spec{Alias: "mine2", Description: "x"})
	if err == nil || err.Error() != "mine is defined outside sshctl and cannot be renamed here" {
		t.Errorf("renaming: err = %v", err)
	}
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Managed: true, Host: Host{HostName: "x"}}); err == nil {
		t.Error("a Spec claiming the user's Host block is managed must not write it")
	}
	if err := tr.cat.Save("", Spec{Alias: "mine", Managed: true, Host: Host{HostName: "x"}}); err == nil || err.Error() != "mine already exists in ssh_config" {
		t.Errorf("adding an alias ssh_config already has: err = %v", err)
	}
	if len(tr.hosts(t)) != 0 || len(tr.config(t).Connections) != 0 {
		t.Error("nothing should be written")
	}
}

// renamable is a tree with Managed connection api (group prod) in History,
// and the user's own Host mine.
func renamable(t *testing.T) *tree {
	t.Helper()
	tr := newTree(t, true, "Host mine\n  HostName example.com\n")
	if err := tr.cat.Save("", Spec{Alias: "api", Group: "prod", Managed: true, Host: Host{HostName: "h"}}); err != nil {
		t.Fatal(err)
	}
	cfg := tr.config(t)
	cfg.History = []string{"api"}
	tr.seed(t, cfg)
	return tr
}

func TestDeleteRemovesManagedHostBlockAndMetadata(t *testing.T) {
	tr := renamable(t)
	if err := tr.cat.Delete("api"); err != nil {
		t.Fatal(err)
	}
	if len(tr.hosts(t)) != 0 {
		t.Errorf("Host blocks = %+v, want none", tr.hosts(t))
	}
	if _, ok := tr.config(t).Connections["api"]; ok {
		t.Error("api's metadata should be deleted with its Host block")
	}
}

func TestDeleteUnmanagedConnectionRemovesOnlyMetadata(t *testing.T) {
	const userConfig = "Host mine\n  HostName example.com\n"
	tr := newTree(t, false, userConfig)
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.config(t).Connections["mine"]; ok {
		t.Error("mine's metadata should be deleted")
	}
	if b, _ := os.ReadFile(tr.ssh.SSHConfig); string(b) != userConfig {
		t.Errorf("the user's ssh_config changed:\n%s", b)
	}
}

func TestFailedHostBlockWriteLeavesMetadataAndHistory(t *testing.T) {
	tr := renamable(t)
	if err := tr.cat.Save("api", Spec{Alias: "mine", Group: "prod", Managed: true, Host: Host{HostName: "h"}}); err == nil {
		t.Fatal("renaming onto the user's alias should fail")
	}
	got := tr.config(t)
	if c, ok := got.Connections["api"]; !ok || c.Group != "prod" || len(got.Connections) != 1 {
		t.Errorf("metadata = %+v, want only api's, unchanged", got.Connections)
	}
	if !slices.Equal(got.History, []string{"api"}) {
		t.Errorf("History = %v, want [api]", got.History)
	}
}

func TestFailedMetadataWriteRestoresTheHostBlock(t *testing.T) {
	tr := renamable(t)
	tr.cat.saveConfig = func(string, *config.Config) error { return errors.New("disk full") }
	err := tr.cat.Save("api", Spec{Alias: "api2", Group: "prod", Managed: true, Host: Host{HostName: "h2"}})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v, want the metadata write's error", err)
	}
	hosts := tr.hosts(t)
	if _, ok := hosts["api2"]; ok || hosts["api"].HostName != "h" {
		t.Errorf("Host blocks = %+v, want api as before so its metadata still matches", hosts)
	}
}
