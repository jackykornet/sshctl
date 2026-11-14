package catalog

import (
	"errors"
	"fmt"
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

func TestDeleteMetadataKeepsTheManagedHostBlock(t *testing.T) {
	tr := renamable(t)
	if err := tr.cat.DeleteMetadata("api"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.config(t).Connections["api"]; ok {
		t.Error("api's metadata should be deleted")
	}
	if h, ok := tr.hosts(t)["api"]; !ok || h.HostName != "h" {
		t.Errorf("Host blocks = %+v, want api's kept", tr.hosts(t))
	}
	if !slices.Equal(tr.config(t).History, []string{"api"}) {
		t.Errorf("History = %v, want [api]", tr.config(t).History)
	}
	if err := tr.cat.DeleteMetadata("api"); !errors.Is(err, ErrNoMetadata) {
		t.Errorf("deleting again: err = %v, want ErrNoMetadata", err)
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

func TestRecordOpenPutsConnectionFirstInHistory(t *testing.T) {
	tr := newTree(t, true, "")
	cfg := config.New()
	for i := range 20 {
		cfg.History = append(cfg.History, fmt.Sprintf("h%02d", i))
	}
	tr.seed(t, cfg)
	if err := tr.cat.RecordOpen("h05"); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).History; len(got) != 20 || got[0] != "h05" || got[6] != "h06" || got[19] != "h19" {
		t.Errorf("History = %v, want h05 moved to the front", got)
	}
	if err := tr.cat.RecordOpen("new"); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).History; len(got) != 20 || got[0] != "new" || got[1] != "h05" || got[19] != "h18" {
		t.Errorf("History = %v, want new first and the oldest dropped to keep 20", got)
	}
}

func TestSetGroupKeepsDescriptionAndDropsEmptiedMetadata(t *testing.T) {
	tr := newTree(t, true, "")
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "site", Group: "prod"}
	cfg.Connections["tmp"] = config.Connection{Alias: "tmp", Group: "scratch"}
	tr.seed(t, cfg)
	for _, mv := range [][2]string{{"web", "staging"}, {"db", "data"}, {"tmp", ""}} {
		if err := tr.cat.SetGroup(mv[0], mv[1]); err != nil {
			t.Fatal(err)
		}
	}
	got := tr.config(t)
	if web := got.Connections["web"]; web.Group != "staging" || web.Description != "site" {
		t.Errorf("web = %+v, want group staging keeping its description", web)
	}
	if db := got.Connections["db"]; db.Alias != "db" || db.Group != "data" {
		t.Errorf("db = %+v, want new metadata in group data", db)
	}
	if _, ok := got.Connections["tmp"]; ok {
		t.Error("tmp has no description or group left and should lose its metadata")
	}
}

func TestSetCollapsedSavesTheCollapsedGroups(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.SetCollapsed([]string{"data", "staging"}); err != nil {
		t.Fatal(err)
	}
	if got, want := tr.config(t).Workspace.CollapsedGroups, []string{"data", "staging"}; !slices.Equal(got, want) {
		t.Errorf("collapsed = %v, want %v", got, want)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"data", "staging"}; !slices.Equal(snap.Collapsed, want) {
		t.Errorf("snapshot collapsed = %v, want %v", snap.Collapsed, want)
	}
}

func TestSnapshotListsConnectionsThenSortedOrphans(t *testing.T) {
	tr := newTree(t, true, "Host mine\n  HostName example.com\n")
	if err := tr.cat.Save("", Spec{Alias: "api", Group: "prod", Managed: true, Host: Host{HostName: "10.0.0.9", Port: "2222"}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop"}); err != nil {
		t.Fatal(err)
	}
	cfg := tr.config(t)
	cfg.DefaultKey = "/k/id"
	cfg.Workspace.CollapsedGroups = []string{"prod"}
	cfg.Connections["zz-gone"] = config.Connection{Alias: "zz-gone", Description: "retired"}
	cfg.Connections["old"] = config.Connection{Alias: "old", Group: "prod"}
	tr.seed(t, cfg)

	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Alias: "api", Group: "prod", Managed: true, Host: Host{HostName: "10.0.0.9", Port: "2222"}},
		{Alias: "mine", Description: "laptop"},
		{Alias: "old", Group: "prod", Orphaned: true},
		{Alias: "zz-gone", Description: "retired", Orphaned: true},
	}
	if !slices.Equal(snap.Entries, want) {
		t.Errorf("entries = %+v, want %+v", snap.Entries, want)
	}
	if snap.Identity != "/k/id" {
		t.Errorf("identity = %q, want /k/id", snap.Identity)
	}
	if !slices.Equal(snap.Collapsed, []string{"prod"}) {
		t.Errorf("collapsed = %v, want [prod]", snap.Collapsed)
	}
}
