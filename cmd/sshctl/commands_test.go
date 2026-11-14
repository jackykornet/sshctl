package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

func TestHelpAndUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "connect <alias>") {
		t.Fatalf("help missing connect usage: %s", out.String())
	}
	if err := run([]string{"not-a-command"}, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("expected unknown command error")
	}
}

// cliTree is an app over a temporary sshctl config and ssh_config, reading in.
func cliTree(t *testing.T, sshConfig string, cfg *config.Config, in string) (*app, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	ssh := &openssh.Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
	if err := os.WriteFile(ssh.SSHConfig, []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sshctl.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return &app{in: strings.NewReader(in), out: &out, err: &out, cfgPath: path, cfg: cfg, ssh: ssh}, &out
}

func TestListShowsAliasesThenSortedOrphanedConnections(t *testing.T) {
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "front end", Group: "prod"}
	cfg.Connections["zz-gone"] = config.Connection{Alias: "zz-gone", Description: "retired"}
	cfg.Connections["old"] = config.Connection{Alias: "old", Group: "prod"}
	cfg.Connections["mid"] = config.Connection{Alias: "mid", Description: "moved"}
	a, out := cliTree(t, "Host web\n  HostName 10.0.0.1\nHost db\n  HostName 10.0.0.2\nHost *\n  User me\n", cfg, "")
	if err := a.list(); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"web                      front end [prod]\n" +
		"db\n" +
		"mid                      moved (orphaned)\n" +
		"old                      [prod] (orphaned)\n" +
		"zz-gone                  retired (orphaned)\n"
	if out.String() != want {
		t.Errorf("list output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A hosts file synced without the Include (ADR-0004) gets a one-line hint.
func TestListHintsAtAMissingInclude(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sshConfig string
		want      bool
	}{
		{"not included", "Host db\n  HostName 10.0.0.2\n", true},
		{"included", openssh.IncludeLine + "\nHost db\n  HostName 10.0.0.2\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, out := cliTree(t, tc.sshConfig, config.New(), "")
			if err := os.WriteFile(a.ssh.ManagedPath(), []byte("Host synced\n  HostName 10.0.0.9\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := a.list(); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			hint := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "does not contain "+openssh.IncludeLine) })
			if got := hint >= 0; got != tc.want {
				t.Fatalf("hint shown %v, want %v:\n%s", got, tc.want, out.String())
			}
			if tc.want && !strings.Contains(lines[hint], "sshctl") {
				t.Errorf("hint %q should say how to fix it", lines[hint])
			}
		})
	}
}

func TestAddWithNoDescriptionOrGroupKeepsNoMetadata(t *testing.T) {
	a, _ := cliTree(t, "Host web\n", config.New(), "\n\n")
	if err := a.metadata([]string{"web"}, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(a.cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := loaded.Connections["web"]; ok {
		t.Errorf("empty add saved metadata %#v, want none", m)
	}
}

func TestEditWithEmptyInputKeepsOldValues(t *testing.T) {
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "front end", Group: "prod"}
	a, _ := cliTree(t, "Host web\n", cfg, "\nstaging\n")
	if err := a.metadata([]string{"web"}, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(a.cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loaded.Connections["web"], (config.Connection{Alias: "web", Description: "front end", Group: "staging"}); got != want {
		t.Errorf("edited metadata = %#v, want %#v", got, want)
	}
}

func TestMetadataPersistsDescriptionAndGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	var out bytes.Buffer
	ssh := &openssh.Client{SSHConfig: filepath.Join(t.TempDir(), "sshconfig")}
	a := &app{in: strings.NewReader("development machine\nengineering\n"), out: &out, cfgPath: path, cfg: c, ssh: ssh}
	if err := a.metadata([]string{"buildbox"}, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Connections["buildbox"]
	if got.Description != "development machine" || got.Group != "engineering" {
		t.Fatalf("unexpected saved metadata: %#v", got)
	}
}

// stale gives a an empty startup config, as when another terminal changed
// the config after sshctl started.
func stale(a *app) { a.cfg = config.New() }

func load(t *testing.T, a *app) *config.Config {
	t.Helper()
	c, err := config.Load(a.cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConnectRecordsHistoryOnTheCurrentConfig(t *testing.T) {
	cfg := config.New()
	cfg.History = []string{"db"}
	a, _ := cliTree(t, "Host web\n", cfg, "")
	stale(a)
	if err := a.recordHistory("web"); err != nil {
		t.Fatal(err)
	}
	if got := load(t, a).History; !slices.Equal(got, []string{"web", "db"}) {
		t.Errorf("History = %v, want [web db]", got)
	}
}

func TestEditStartsFromTheCurrentMetadata(t *testing.T) {
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "front end", Group: "prod"}
	a, out := cliTree(t, "Host web\n", cfg, "\n\n")
	stale(a)
	if err := a.metadata([]string{"web"}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Description [front end]: Group [prod]: ") {
		t.Errorf("prompts = %q, want the current values", out.String())
	}
	if got := load(t, a).Connections["web"]; got.Description != "front end" || got.Group != "prod" {
		t.Errorf("web = %#v, want its metadata unchanged", got)
	}
}

func TestRemoveDeletesOnlyMetadata(t *testing.T) {
	cfg := config.New()
	cfg.Connections["api"] = config.Connection{Alias: "api", Group: "prod"}
	a, _ := cliTree(t, openssh.IncludeLine+"\n", cfg, "")
	if err := a.ssh.SaveHost("", openssh.Host{Alias: "api", Endpoint: openssh.Endpoint{HostName: "10.0.0.9"}}); err != nil {
		t.Fatal(err)
	}
	stale(a)
	if err := a.remove([]string{"api"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := load(t, a).Connections["api"]; ok {
		t.Error("api's metadata should be removed")
	}
	if hosts, err := a.ssh.ManagedHosts(); err != nil || len(hosts) != 1 || hosts[0].Alias != "api" {
		t.Errorf("Host blocks = %+v, %v; want api's kept", hosts, err)
	}
	if err := a.remove([]string{"api"}); err == nil || err.Error() != `no sshctl metadata for "api"` {
		t.Errorf("removing again: err = %v", err)
	}
}
