package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// fakeHome points the home directory and the config folders at a temporary
// directory, so run never reads or moves the real config.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	return home
}

func TestRunMovesTheOldConfigOnceWithANotice(t *testing.T) {
	home := fakeHome(t)
	var old string
	switch runtime.GOOS {
	case "darwin":
		old = filepath.Join(home, "Library", "Application Support", "sshctl")
	case "windows":
		old = filepath.Join(home, "AppData", "Roaming", "sshctl")
	default:
		t.Skip("no old config folder on " + runtime.GOOS)
	}
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "config.json"), []byte(`{"version":3}`), 0600); err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false} {
		var out, errOut bytes.Buffer
		if err := run([]string{"config", "show"}, strings.NewReader(""), &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(errOut.String(), "moved the config"); got != want {
			t.Errorf("run %d: stderr = %q, want notice %v", i+1, errOut.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "sshctl", "config.json")); err != nil {
		t.Fatal(err)
	}
}

// The Selected identity is read and written through the Connection catalog,
// so a change made elsewhere since sshctl started is what the CLI sees.
func TestSelectedIdentityGoesThroughTheCatalog(t *testing.T) {
	home := fakeHome(t)
	key := filepath.Join(home, ".ssh", "id_work")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{key, key + ".pub"} {
		if err := os.WriteFile(f, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, out := cliTree(t, "", config.New(), "")
	if err := a.catalog().SetIdentity(key); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"config", "show"}); err != nil {
		t.Fatal(err)
	}
	if want := "Selected identity: " + key + "\n"; !strings.Contains(out.String(), want) {
		t.Errorf("config show = %q, want %q", out, want)
	}
	if strings.Contains(strings.ToLower(out.String()), "default key") {
		t.Errorf("config show says default key: %q", out)
	}
	out.Reset()
	if err := a.execute([]string{"key", "list"}); err != nil {
		t.Fatal(err)
	}
	if want := key + ".pub (Selected identity)\n"; !strings.Contains(out.String(), want) {
		t.Errorf("key list = %q, want %q", out, want)
	}
	if err := a.execute([]string{"key", "use", "none"}); err != nil {
		t.Fatal(err)
	}
	if snap, err := a.catalog().Snapshot(); err != nil || snap.Identity != "" {
		t.Errorf("after key use none: Identity = %q, %v; want none", snap.Identity, err)
	}
}

func TestHelpAndUnknownCommand(t *testing.T) {
	fakeHome(t)
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

// Every way to call a command that the table declares is dispatched to it
// and listed in --help and in its own --help.
func TestEveryCommandIsReachableAndInHelp(t *testing.T) {
	var help bytes.Buffer
	if err := run([]string{"--help"}, strings.NewReader(""), &help, &help); err != nil {
		t.Fatal(err)
	}
	a, out := cliTree(t, "", config.New(), "")
	for i := range commands {
		c := &commands[i]
		out.Reset()
		if err := a.execute([]string{c.name, "--help"}); err != nil {
			t.Fatal(err)
		}
		for j := range c.forms {
			f := &c.forms[j]
			calls := [][]string{{c.name}}
			if c.inSpace {
				calls = append(calls, []string{c.name, "--space", "x"})
			}
			for _, call := range calls {
				if f.sub != "" {
					call = append(call, f.sub)
				}
				gotC, gotF, _ := lookup(call)
				if gotC != c || gotF != f {
					t.Errorf("sshctl %s does not reach its form", strings.Join(call, " "))
				}
			}
			name := strings.TrimSpace(c.name + " " + f.sub)
			if !strings.Contains(help.String(), "\n  "+name) {
				t.Errorf("--help does not list %q:\n%s", name, help.String())
			}
			if !strings.Contains(out.String(), f.args) {
				t.Errorf("sshctl %s --help = %q, lacks %q", c.name, out.String(), f.args)
			}
		}
	}
	if gotC, _, _ := lookup([]string{"not-a-command"}); gotC != nil {
		t.Errorf("an unknown command reached %q", gotC.name)
	}
}

// A command called with no subcommand, an unknown one, or arguments that do
// not fit gets its usage.
func TestWrongCallsGetTheCommandsUsage(t *testing.T) {
	a, _ := cliTree(t, "", config.New(), "")
	for _, args := range [][]string{
		{"agent"}, {"key"}, {"key", "bogus"}, {"config", "bogus"},
		{"group", "--space", "default"}, {"group", "add"}, {"space", "rename", "default"},
		{"list", "extra"}, {"bootstrap"},
	} {
		err := a.execute(args)
		if want := "usage: sshctl " + usage(args[0]); err == nil || err.Error() != want {
			t.Errorf("sshctl %s: err = %v, want %q", strings.Join(args, " "), err, want)
		}
	}
}

// The README's Commands block lists every form --help does, in its order.
func TestReadmeListsTheCommands(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows checks README.md out with CRLF line endings (.gitattributes).
	text := strings.ReplaceAll(string(readme), "\r\n", "\n")
	_, block, ok := strings.Cut(text, "## Commands\n\n```text\n")
	if ok {
		block, _, ok = strings.Cut(block, "```")
	}
	if !ok {
		t.Fatal("README has no Commands block")
	}
	want := "sshctl\n"
	for _, c := range commands {
		for _, f := range c.forms {
			want += "sshctl " + c.synopsis(f) + "\n"
		}
	}
	if block != want {
		t.Errorf("README Commands block:\n%s\nwant, from the command table:\n%s", block, want)
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
	// The commands' own tests run with every Experimental feature on;
	// experimental_test.go turns them off.
	cfg.Experimental = config.Experimental{Bootstrap: true, Provision: true}
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
	if err := a.list(nil); err != nil {
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
			if err := a.list(nil); err != nil {
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
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "staging"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "front end", Group: "prod"}
	a, _ := cliTree(t, "Host web\n", cfg, "\nstaging\n")
	if err := a.metadata([]string{"web"}, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(a.cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loaded.Connections["web"], (config.Connection{Alias: "web", Description: "front end", Group: "staging"}); !reflect.DeepEqual(got, want) {
		t.Errorf("edited metadata = %#v, want %#v", got, want)
	}
}

func TestMetadataPersistsDescriptionAndGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	c.Groups = []config.Group{{Name: "engineering"}}
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
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

// history reads History through the Connection catalog, so an open recorded
// elsewhere since sshctl started is listed.
func TestHistoryReadsTheCurrentConfig(t *testing.T) {
	cfg := config.New()
	cfg.History = []string{"web", "db"}
	a, out := cliTree(t, "Host web\n", cfg, "")
	stale(a)
	if err := a.execute([]string{"history"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "web\ndb\n" {
		t.Errorf("history = %q, want web then db", out.String())
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
