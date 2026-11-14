package openssh

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func managedClient(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	return &Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
}

func TestSaveHostRoundTripsAndKeepsExtraOptions(t *testing.T) {
	c := includingClient(t, "")
	web := Host{Alias: "web", Endpoint: Endpoint{HostName: "10.0.0.1", User: "deploy", Port: "2222", IdentityFile: "~/.ssh/id work"}}
	if err := c.SaveHost("", web); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveHost("", Host{Alias: "db", Endpoint: Endpoint{HostName: "db.internal"}}); err != nil {
		t.Fatal(err)
	}
	// The user adds an option by hand; sshctl must keep it on the next save.
	b, _ := os.ReadFile(c.ManagedPath())
	b = []byte(strings.Replace(string(b), "    Port 2222\n", "    Port 2222\n    ForwardAgent yes\n", 1))
	if err := os.WriteFile(c.ManagedPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveHost("web", Host{Alias: "web2", Endpoint: Endpoint{HostName: "10.0.0.2", IdentityFile: "~/.ssh/id work"}}); err != nil {
		t.Fatal(err)
	}
	got, err := c.ManagedHosts()
	if err != nil {
		t.Fatal(err)
	}
	want := []Host{
		{Alias: "web2", Endpoint: Endpoint{HostName: "10.0.0.2", IdentityFile: "~/.ssh/id work"}, Extra: []string{"ForwardAgent yes"}},
		{Alias: "db", Endpoint: Endpoint{HostName: "db.internal"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %+v\nwant    %+v", got, want)
	}
	aliases, err := HostAliases(c.ManagedPath())
	if err != nil || !reflect.DeepEqual(aliases, []string{"web2", "db"}) {
		t.Fatalf("OpenSSH-style aliases = %v (%v)", aliases, err)
	}
	if fi, _ := os.Stat(c.ManagedPath()); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("managed file mode = %v, want owner-only", fi.Mode().Perm())
	}
}

func TestSaveHostRejectsDuplicatesAndUnknownHosts(t *testing.T) {
	c := includingClient(t, "")
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "a"}}); err != nil {
		t.Fatal(err)
	}
	var exists *AliasExistsError
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "b"}}); !errors.As(err, &exists) {
		t.Errorf("adding a second web: err = %v, want an AliasExistsError", err)
	}
	if err := c.SaveHost("nope", Host{Alias: "x", Endpoint: Endpoint{HostName: "b"}}); !errors.Is(err, ErrUnknownHost) {
		t.Errorf("err = %v, want ErrUnknownHost", err)
	}
	if err := c.DeleteHost("nope"); !errors.Is(err, ErrUnknownHost) {
		t.Errorf("err = %v, want ErrUnknownHost", err)
	}
	if err := c.DeleteHost("web"); err != nil {
		t.Fatal(err)
	}
	if hosts, _ := c.ManagedHosts(); len(hosts) != 0 {
		t.Errorf("hosts = %v, want none", hosts)
	}
}

func TestHostValidateRejectsValuesThatChangeTheConfig(t *testing.T) {
	ok := Host{Alias: "web", Endpoint: Endpoint{HostName: "h"}}
	tests := []struct {
		name string
		edit func(*Host)
	}{
		{"no alias", func(h *Host) { h.Alias = "" }},
		{"pattern alias", func(h *Host) { h.Alias = "web*" }},
		{"two aliases", func(h *Host) { h.Alias = "a b" }},
		{"option alias", func(h *Host) { h.Alias = "-oProxyCommand" }},
		{"no hostname", func(h *Host) { h.HostName = "" }},
		{"hostname with space", func(h *Host) { h.HostName = "h ProxyCommand x" }},
		{"newline", func(h *Host) { h.User = "u\nProxyCommand x" }},
		{"port text", func(h *Host) { h.Port = "ssh" }},
		{"port range", func(h *Host) { h.Port = "70000" }},
		{"quote in identity", func(h *Host) { h.IdentityFile = `a"b` }},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid host: %v", err)
	}
	for _, tt := range tests {
		h := ok
		tt.edit(&h)
		if err := h.Validate(); err == nil {
			t.Errorf("%s: want an error", tt.name)
		}
	}
}

func TestAddIncludeMakesConfigIncludeManagedFile(t *testing.T) {
	c := managedClient(t)
	if ok, err := c.IncludesManaged(); ok || err != nil {
		t.Fatalf("missing config: includes = %v, %v", ok, err)
	}
	original := "Host mine\n  HostName example.com\n"
	if err := os.WriteFile(c.SSHConfig, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.IncludesManaged(); ok {
		t.Fatal("config without the Include should not count")
	}
	if err := c.AddInclude(); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.IncludesManaged(); !ok || err != nil {
		t.Fatalf("after AddInclude: includes = %v, %v", ok, err)
	}
	b, _ := os.ReadFile(c.SSHConfig)
	if !strings.HasSuffix(string(b), original) {
		t.Errorf("original config must follow the Include unchanged:\n%s", b)
	}
	if backup, _ := os.ReadFile(c.SSHConfig + ".sshctl-backup"); string(backup) != original {
		t.Errorf("backup = %q", backup)
	}
	if err := c.SaveHost("", Host{Alias: "managed", Endpoint: Endpoint{HostName: "m"}}); err != nil {
		t.Fatal(err)
	}
	aliases, err := c.Aliases()
	if err != nil || !reflect.DeepEqual(aliases, []string{"managed", "mine"}) {
		t.Fatalf("aliases = %v (%v)", aliases, err)
	}
}

func TestIncludesManagedIgnoresIncludeInsideHostBlock(t *testing.T) {
	c := managedClient(t)
	if err := os.WriteFile(c.SSHConfig, []byte("Host x\n  Include sshctl_config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.IncludesManaged(); ok {
		t.Fatal("an Include inside a Host block only applies to that host")
	}
	if err := os.WriteFile(c.SSHConfig, []byte("Include ~/.ssh/other "+filepath.Join(c.SSHDir, "sshctl_*")+"\nHost x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.IncludesManaged(); !ok {
		t.Fatal("a top-level glob matching the managed file counts")
	}
}

// includingClient is a Client whose ssh_config Includes the managed file
// followed by config, the user's own directives.
func includingClient(t *testing.T, config string) *Client {
	t.Helper()
	c := managedClient(t)
	if err := os.WriteFile(c.SSHConfig, []byte(IncludeLine+"\n"+config), 0o600); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSaveHostRefusesAliasInUsersConfig(t *testing.T) {
	c := includingClient(t, "Host mine\n  HostName example.com\n")
	err := c.SaveHost("", Host{Alias: "mine", Endpoint: Endpoint{HostName: "other"}})
	var exists *AliasExistsError
	if !errors.As(err, &exists) || exists.Alias != "mine" {
		t.Fatalf("err = %v, want an AliasExistsError for mine", err)
	}
	if hosts, _ := c.ManagedHosts(); len(hosts) != 0 {
		t.Errorf("hosts = %v, want none written", hosts)
	}
}

func TestSaveHostRefusesAliasInIncludedFile(t *testing.T) {
	c := includingClient(t, "Include conf.d/*\n")
	if err := os.MkdirAll(filepath.Join(c.SSHDir, "conf.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.SSHDir, "conf.d", "team"), []byte("Host bastion\n  HostName b.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "w"}}); err != nil {
		t.Fatal(err)
	}
	err := c.SaveHost("web", Host{Alias: "bastion", Endpoint: Endpoint{HostName: "w"}})
	var exists *AliasExistsError
	if !errors.As(err, &exists) || exists.Alias != "bastion" {
		t.Fatalf("renaming onto an included alias: err = %v, want an AliasExistsError for bastion", err)
	}
}

func TestSaveHostKeepingItsAliasIsNotADuplicate(t *testing.T) {
	c := includingClient(t, "Host mine\n  HostName example.com\n")
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "a"}}); err != nil {
		t.Fatal(err)
	}
	if aliases, _ := c.Aliases(); !slices.Contains(aliases, "web") {
		t.Fatalf("aliases = %v, want web read through the Include", aliases)
	}
	if err := c.SaveHost("web", Host{Alias: "web", Endpoint: Endpoint{HostName: "b", Port: "2222"}}); err != nil {
		t.Fatalf("saving web as web: %v", err)
	}
	got, _ := c.ManagedHosts()
	if want := []Host{{Alias: "web", Endpoint: Endpoint{HostName: "b", Port: "2222"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %+v, want %+v", got, want)
	}
}

func TestSaveHostRefusesInvalidValuesBeforeWriting(t *testing.T) {
	c := includingClient(t, "")
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "h ProxyCommand x"}}); err == nil {
		t.Fatal("want an error for a hostname that injects a directive")
	}
	if _, err := os.Stat(c.ManagedPath()); !os.IsNotExist(err) {
		t.Errorf("managed file written for an invalid host (stat err %v)", err)
	}
}

func TestSaveHostNeedsConfigToIncludeManagedFile(t *testing.T) {
	c := managedClient(t)
	if err := os.WriteFile(c.SSHConfig, []byte("Host mine\n  HostName example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "w"}}); !errors.Is(err, ErrNotIncluded) {
		t.Fatalf("err = %v, want ErrNotIncluded", err)
	}
	if _, err := os.Stat(c.ManagedPath()); !os.IsNotExist(err) {
		t.Errorf("managed file written before ssh_config reads it (stat err %v)", err)
	}
	if err := c.AddInclude(); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveHost("", Host{Alias: "web", Endpoint: Endpoint{HostName: "w"}}); err != nil {
		t.Fatalf("after AddInclude: %v", err)
	}
}
