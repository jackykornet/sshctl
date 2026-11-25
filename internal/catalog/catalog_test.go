package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	tr.addGroups(t, "prod")
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

func TestSaveStoresIdentityFileUnderHomeWithTilde(t *testing.T) {
	tr := newTree(t, true, "")
	tr.ssh.Home = filepath.Join(t.TempDir(), "jo")
	key := filepath.Join(tr.ssh.Home, ".ssh", "id_ed25519")
	if err := tr.cat.Save("", Spec{Alias: "api", Managed: true, Host: Host{HostName: "h", IdentityFile: key}}); err != nil {
		t.Fatal(err)
	}
	entry := func() Entry {
		t.Helper()
		snap, err := tr.cat.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		return snap.Entries[0]
	}
	e := entry()
	if e.Host.IdentityFile != "~/.ssh/id_ed25519" {
		t.Fatalf("edit form would show IdentityFile %q, want ~/.ssh/id_ed25519", e.Host.IdentityFile)
	}
	if err := tr.cat.Save("api", Spec{Alias: e.Alias, Managed: true, Host: e.Host}); err != nil {
		t.Fatal(err)
	}
	if got := entry().Host.IdentityFile; got != "~/.ssh/id_ed25519" {
		t.Fatalf("after saving the edit unchanged IdentityFile = %q", got)
	}
}

func TestSaveAsksForIncludeBeforeWritingAnything(t *testing.T) {
	tr := newTree(t, false, "Host mine\n  HostName example.com\n")
	tr.addGroups(t, "prod")
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

// addGroups creates the Groups names, in order.
func (tr *tree) addGroups(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := tr.cat.AddGroup("", name); err != nil {
			t.Fatal(err)
		}
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
	tr.addGroups(t, "prod")
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
	tr.addGroups(t, "home")
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
	err = tr.cat.Save("mine", Spec{Alias: "mine", Managed: true, Host: Host{HostName: "x"}})
	if err == nil || err.Error() != "mine's Host block is defined outside sshctl; only its description and group can be edited here" {
		t.Errorf("a Spec claiming the user's Host block is managed: err = %v", err)
	}
	if err := tr.cat.Save("", Spec{Alias: "mine", Managed: true, Host: Host{HostName: "x"}}); err == nil || err.Error() != "mine already exists in ssh_config" {
		t.Errorf("adding an alias ssh_config already has: err = %v", err)
	}
	if err := tr.cat.Save("gone", Spec{Alias: "gone2", Description: "x"}); err == nil || err.Error() != "gone has no Host block in ssh_config to rename" {
		t.Errorf("renaming an alias ssh_config lacks: err = %v", err)
	}
	if len(tr.hosts(t)) != 0 || len(tr.config(t).Connections) != 0 {
		t.Error("nothing should be written")
	}
}

// renamable is a tree with Managed connection api (group prod) in History,
// the user's own Host mine, and Groups prod and staging.
func renamable(t *testing.T) *tree {
	t.Helper()
	tr := newTree(t, true, "Host mine\n  HostName example.com\n")
	tr.addGroups(t, "prod", "staging")
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

// unchanged fails unless tr still holds what renamable wrote.
func (tr *tree) unchanged(t *testing.T) {
	t.Helper()
	got := tr.config(t)
	if c, ok := got.Connections["api"]; !ok || c.Group != "prod" || len(got.Connections) != 1 {
		t.Errorf("metadata = %+v, want only api's, unchanged", got.Connections)
	}
	if !slices.Equal(got.History, []string{"api"}) {
		t.Errorf("History = %v, want [api]", got.History)
	}
	if hosts := tr.hosts(t); len(hosts) != 1 || hosts["api"].HostName != "h" {
		t.Errorf("Host blocks = %+v, want only api's, unchanged", hosts)
	}
}

func TestRefusedHostBlockLeavesMetadataAndHistory(t *testing.T) {
	tr := renamable(t)
	if err := tr.cat.Save("api", Spec{Alias: "mine", Group: "prod", Managed: true, Host: Host{HostName: "h"}}); err == nil {
		t.Fatal("renaming onto the user's alias should fail")
	}
	tr.unchanged(t)
}

func TestFailedHostBlockWriteLeavesMetadataAndHistory(t *testing.T) {
	tr := renamable(t)
	tr.cat.saveHostBlock = func(string, openssh.Host) error { return errors.New("disk full") }
	err := tr.cat.Save("api", Spec{Alias: "api2", Description: "API", Group: "staging", Managed: true, Host: Host{HostName: "h2"}})
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("err = %v, want the Host block write's error", err)
	}
	tr.unchanged(t)
}

func TestSaveEditsManagedConnectionKeepingItsOtherDirectives(t *testing.T) {
	tr := renamable(t)
	f, err := os.OpenFile(tr.ssh.ManagedPath(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("    ForwardAgent yes\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := tr.cat.Save("api", Spec{Alias: "api", Description: "API", Group: "staging", Managed: true, Host: Host{HostName: "h2", User: "deploy"}}); err != nil {
		t.Fatal(err)
	}
	h := tr.hosts(t)["api"]
	if h.HostName != "h2" || h.User != "deploy" || !slices.Equal(h.Extra, []string{"ForwardAgent yes"}) {
		t.Errorf("api Host block = %+v, want h2, deploy and ForwardAgent kept", h)
	}
	got := tr.config(t)
	if c := got.Connections["api"]; c.Description != "API" || c.Group != "staging" {
		t.Errorf("api metadata = %+v", c)
	}
	if !slices.Equal(got.History, []string{"api"}) {
		t.Errorf("History = %v, want [api]", got.History)
	}
}

func TestDeleteLeavesTheUsersHostBlockWhenIncluded(t *testing.T) {
	tr := renamable(t)
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(tr.ssh.SSHConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.config(t).Connections["mine"]; ok {
		t.Error("mine's metadata should be deleted")
	}
	if b, _ := os.ReadFile(tr.ssh.SSHConfig); string(b) != string(before) {
		t.Errorf("the user's ssh_config changed:\n%s", b)
	}
	if hosts := tr.hosts(t); len(hosts) != 1 || hosts["api"].HostName != "h" {
		t.Errorf("Host blocks = %+v, want only api's, unchanged", hosts)
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
	cfg.Groups = []config.Group{{Name: "staging"}, {Name: "data"}}
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
	if got, want := tr.config(t).CollapsedGroups, []string{"data", "staging"}; !slices.Equal(got, want) {
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

func TestKeysListsSSHDirPublicKeysAndRegisteredKeys(t *testing.T) {
	tr := newTree(t, true, "")
	d := tr.ssh.SSHDir
	for _, n := range []string{"id_ed25519", "id_ed25519.pub", "work.pub", "work-cert.pub", "known_hosts"} {
		if err := os.WriteFile(filepath.Join(d, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := tr.config(t)
	c.Keys = []string{"/k/deploy", filepath.Join(d, "work.pub")}
	if err := config.Save(tr.cfg, c); err != nil {
		t.Fatal(err)
	}
	got, err := tr.cat.Keys()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/k/deploy", filepath.Join(d, "id_ed25519.pub"), filepath.Join(d, "work-cert.pub"), filepath.Join(d, "work.pub")}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
}

func TestSetIdentitySelectsTheKey(t *testing.T) {
	tr := newTree(t, true, "")
	for _, key := range []string{"/k/id_ed25519", ""} {
		if err := tr.cat.SetIdentity(key); err != nil {
			t.Fatal(err)
		}
		snap, err := tr.cat.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snap.Identity != key {
			t.Errorf("identity = %q, want %q", snap.Identity, key)
		}
	}
}

func TestSyncedConfigLeavesThisMachinesStateAlone(t *testing.T) {
	tr := newTree(t, true, "Host web\n")
	tr.addGroups(t, "prod")
	if err := tr.cat.Save("web", Spec{Alias: "web", Group: "prod"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.RecordOpen("web"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetCollapsed([]string{"prod"}); err != nil {
		t.Fatal(err)
	}
	synced, err := os.ReadFile(tr.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Another machine's config.json, as chezmoi apply writes it.
	other := `{"version":3,"groups":[{"name":"prod"}],"connections":{"web":{"alias":"web","description":"from elsewhere","group":"prod"}}}`
	if err := os.WriteFile(tr.cfg, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(snap.Collapsed, []string{"prod"}) || snap.Entries[0].Description != "from elsewhere" {
		t.Errorf("snapshot = %+v, want the synced description and this machine's collapsed groups", snap)
	}
	if got := tr.config(t).History; !slices.Equal(got, []string{"web"}) {
		t.Errorf("History = %v, want [web]", got)
	}
	if strings.Contains(string(synced), "history") || strings.Contains(string(synced), "collapsed") {
		t.Errorf("config.json holds machine-local state:\n%s", synced)
	}
}

func TestSnapshotListsConnectionsThenSortedOrphans(t *testing.T) {
	tr := newTree(t, true, "Host mine\n  HostName example.com\n")
	tr.addGroups(t, "prod")
	if err := tr.cat.Save("", Spec{Alias: "api", Group: "prod", Managed: true, Host: Host{HostName: "10.0.0.9", Port: "2222"}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("mine", Spec{Alias: "mine", Description: "laptop"}); err != nil {
		t.Fatal(err)
	}
	cfg := tr.config(t)
	cfg.SelectedIdentity = "/k/id"
	cfg.CollapsedGroups = []string{"prod"}
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
	if !reflect.DeepEqual(snap.Entries, want) {
		t.Errorf("entries = %+v, want %+v", snap.Entries, want)
	}
	if snap.Identity != "/k/id" {
		t.Errorf("identity = %q, want /k/id", snap.Identity)
	}
	if !slices.Equal(snap.Collapsed, []string{"prod"}) {
		t.Errorf("collapsed = %v, want [prod]", snap.Collapsed)
	}
}

func TestSetGroupRefusesAGroupWithNoRecord(t *testing.T) {
	tr := newTree(t, true, "")
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
	tr.seed(t, cfg)
	if err := tr.cat.SetGroup("web", "nowhere"); !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("err = %v, want ErrUnknownGroup", err)
	}
	got := tr.config(t)
	if web := got.Connections["web"]; web.Group != "prod" {
		t.Errorf("web = %+v, want it left in prod", web)
	}
	if got.GroupIndex("", "nowhere") >= 0 {
		t.Error("a refused move must not create the group")
	}
}

func TestGroupOutlivesItsLastMemberInStoredOrder(t *testing.T) {
	tr := newTree(t, true, "Host web\nHost db\n")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "zeta"}, {Name: "prod"}, {Name: "alpha"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
	cfg.Connections["db"] = config.Connection{Alias: "db", Group: "alpha"}
	tr.seed(t, cfg)
	if err := tr.cat.SetGroup("web", "alpha"); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if want := []Group{{Name: "zeta"}, {Name: "prod"}, {Name: "alpha"}}; !slices.Equal(snap.Groups, want) {
		t.Errorf("groups = %+v, want %+v, empty ones included", snap.Groups, want)
	}
}

func TestSaveRefusesAnAllDigitGroup(t *testing.T) {
	tr := newTree(t, true, "")
	err := tr.cat.Save("", Spec{Alias: "api", Group: "42", Managed: true, Host: Host{HostName: "h"}})
	if !errors.Is(err, config.ErrGroupName) {
		t.Fatalf("err = %v, want ErrGroupName", err)
	}
	if _, ok := tr.hosts(t)["api"]; ok {
		t.Error("a refused Save must not write the Host block")
	}
}

// A hosts file synced to a new machine is invisible to ssh until the user's
// ssh_config Includes it (ADR-0004).
func TestSnapshotReportsAnUnincludedHostsFile(t *testing.T) {
	for _, tc := range []struct {
		name               string
		included, hostFile bool
		want               bool
	}{
		{"hosts file not included", false, true, true},
		{"hosts file included", true, true, false},
		{"no hosts file", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, tc.included, "Host user-box\n    HostName 10.0.0.1\n")
			if tc.hostFile {
				if err := os.WriteFile(tr.ssh.ManagedPath(), []byte("Host synced\n    HostName 10.0.0.2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			snap, err := tr.cat.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if got := snap.Include != nil; got != tc.want {
				t.Fatalf("Include = %+v, want set %v", snap.Include, tc.want)
			}
			if tc.want && snap.Include.Line != openssh.IncludeLine {
				t.Fatalf("Include.Line = %q, want %q", snap.Include.Line, openssh.IncludeLine)
			}
		})
	}
}

func TestProvisionRepoTakesTheGroupsRepoThenTheGlobalOne(t *testing.T) {
	const global, group = "https://example.com/global.git", "https://example.com/prod.git"
	for _, tc := range []struct {
		name          string
		global, group string
		member        bool
		want          string
		err           error
	}{
		{"group repo wins", global, group, true, group, nil},
		{"unset group falls back to global", global, "", true, global, nil},
		{"ungrouped uses global", global, group, false, global, nil},
		{"group none refuses despite global", global, config.DotfilesNone, true, "", ErrProvisionRefused},
		{"global none refuses", config.DotfilesNone, "", true, "", ErrProvisionRefused},
		{"group repo without global", "", group, true, group, nil},
		{"nothing set", "", "", true, "", ErrNoDotfiles},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, true, "Host web\n")
			cfg := config.New()
			cfg.Provision.Dotfiles = tc.global
			cfg.Groups = []config.Group{{Name: "prod", Dotfiles: tc.group}}
			if tc.member {
				cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
			}
			tr.seed(t, cfg)
			got, err := tr.cat.ProvisionRepo("web")
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Errorf("ProvisionRepo = %q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestProvisionRefusedByAGroupNamesTheGroup(t *testing.T) {
	tr := newTree(t, true, "Host web\n")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "prod", Dotfiles: config.DotfilesNone}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod"}
	tr.seed(t, cfg)
	if _, err := tr.cat.ProvisionRepo("web"); err == nil || !strings.Contains(err.Error(), "prod") {
		t.Errorf("err = %v, want one naming group prod", err)
	}
}

func TestSetGroupDotfilesSavesOnTheGroupRecord(t *testing.T) {
	const repo = "https://example.com/prod.git"
	tr := newTree(t, true, "")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "dev"}, {Name: "prod"}}
	tr.seed(t, cfg)
	if err := tr.cat.SetGroupDotfiles("", "prod", repo); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if want := []Group{{Name: "dev"}, {Name: "prod", Dotfiles: repo}}; !slices.Equal(snap.Groups, want) {
		t.Errorf("groups = %+v, want %+v", snap.Groups, want)
	}
	if err := tr.cat.SetGroupDotfiles("", "prod", ""); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).Groups[1]; got.Dotfiles != "" {
		t.Errorf("after unset prod = %+v", got)
	}
}

func TestSetGroupDotfilesRefusesAGroupWithNoRecord(t *testing.T) {
	tr := newTree(t, true, "")
	tr.seed(t, config.New())
	if err := tr.cat.SetGroupDotfiles("", "nowhere", config.DotfilesNone); !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("err = %v, want ErrUnknownGroup", err)
	}
	if got := tr.config(t).Groups; len(got) != 0 {
		t.Errorf("groups = %+v, want none created", got)
	}
}

// A dangling sshctl_config symlink, as a dotfiles manager leaves before it
// has applied its target, still counts as sshctl's hosts file.
func TestSnapshotReportsAnUnincludedDanglingHostsLink(t *testing.T) {
	tr := newTree(t, false, "Host user-box\n")
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), tr.ssh.ManagedPath()); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Include == nil {
		t.Fatal("Include = nil, want the missing Include offered")
	}
}

func TestDisplayPathUsesForwardSlashesUnderHome(t *testing.T) {
	for _, tc := range []struct{ path, home, want string }{
		{"/home/jo/.ssh/config", "/home/jo", "~/.ssh/config"},
		{`C:\Users\jo\.ssh\config`, `C:\Users\jo`, "~/.ssh/config"},
		{"/etc/ssh/ssh_config", "/home/jo", "/etc/ssh/ssh_config"},
	} {
		if got := displayPath(tc.path, tc.home); got != tc.want {
			t.Errorf("displayPath(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
		}
	}
}

func TestSnapshotFindsAnEntryAndAGroup(t *testing.T) {
	tr := newTree(t, true, "Host web\n")
	cfg := config.New()
	cfg.Groups = []config.Group{{Name: "prod"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Group: "prod", Description: "front"}
	cfg.Connections["gone"] = config.Connection{Alias: "gone", Description: "retired"}
	tr.seed(t, cfg)
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := snap.Entry("web"); !ok || !reflect.DeepEqual(e, Entry{Alias: "web", Group: "prod", Description: "front"}) {
		t.Errorf("Entry(web) = %+v, %v", e, ok)
	}
	if e, ok := snap.Entry("gone"); !ok || !e.Orphaned {
		t.Errorf("Entry(gone) = %+v, %v, want the Orphaned connection", e, ok)
	}
	if e, ok := snap.Entry("nope"); ok {
		t.Errorf("Entry(nope) = %+v, want none", e)
	}
	if !snap.HasGroup("", "prod") || snap.HasGroup("", "dev") || snap.HasGroup("", "") {
		t.Errorf("HasGroup: prod %v, dev %v, empty %v; want only prod", snap.HasGroup("", "prod"), snap.HasGroup("", "dev"), snap.HasGroup("", ""))
	}
}

func TestSetFeatureTurnsAnExperimentalFeatureOnAndOff(t *testing.T) {
	tr := newTree(t, true, "Host web\n")
	if err := tr.cat.CheckFeature(FeatureBootstrap); !errors.Is(err, config.ErrFeatureOff) {
		t.Fatalf("CheckFeature before SetFeature = %v, want ErrFeatureOff", err)
	}
	if err := tr.cat.SetFeature(FeatureBootstrap, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.CheckFeature(FeatureBootstrap); err != nil {
		t.Errorf("CheckFeature after enable = %v", err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Experimental.On(FeatureBootstrap) || snap.Experimental.On(FeatureProvision) {
		t.Errorf("Snapshot.Experimental = %+v, want only bootstrap on", snap.Experimental)
	}
	if err := tr.cat.SetFeature(FeatureBootstrap, false); err != nil {
		t.Fatal(err)
	}
	if tr.config(t).Experimental.On(FeatureBootstrap) {
		t.Error("bootstrap still on after disable")
	}
	if err := tr.cat.SetFeature("sync", true); !errors.Is(err, config.ErrUnknownFeature) {
		t.Errorf("SetFeature(sync) = %v, want ErrUnknownFeature", err)
	}
}
