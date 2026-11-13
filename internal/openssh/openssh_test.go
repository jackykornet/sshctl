package openssh

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAliasesIgnoresPatternsAndDuplicates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte("Host web api\nHost *.example.com !bad\nHost api db # repeated\nHost=eq-host\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := HostAliases(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "api", "db", "eq-host"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAliasesFollowsIncludes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("config", "Host first\nInclude config.d/* extra missing.conf\nHost last\n")
	write("config.d/b.conf", "Host b-host\n")
	write("config.d/a.conf", "Host a-host\nInclude nested\n")
	write("nested", "Host nested-host first\n")
	write("extra", "Include="+filepath.Join(dir, "abs")+"\n")
	write("abs", "Host abs-host\n")

	c := &Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
	got, err := c.Aliases()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "a-host", "nested-host", "b-host", "abs-host", "last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAliasesRejectsIncludeLoops(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte("Include config\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{SSHConfig: p, SSHDir: dir}
	if _, err := c.Aliases(); err == nil {
		t.Fatal("expected an error for recursive includes")
	}
}

func TestSafeSettingsFollowsIncludes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("config", "Host first\n  User alice\nInclude config.d/* missing.conf\nHost last\n  Port 2222\n")
	write("config.d/b.conf", "Host b-host\n  HostName b.example.com\n")
	write("config.d/a.conf", "Host a-host\n  ProxyCommand secret\nInclude nested\n")
	write("nested", "Host nested-host\n  IdentityFile ~/.ssh/id_nested\n")

	c := &Client{SSHConfig: filepath.Join(dir, "config"), SSHDir: dir}
	got, err := c.SafeSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Host first", "User alice",
		"Host a-host",
		"Host nested-host", "IdentityFile ~/.ssh/id_nested",
		"Host b-host", "HostName b.example.com",
		"Host last", "Port 2222",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSafeSettingsRejectsIncludeLoops(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte("Include config\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{SSHConfig: p, SSHDir: dir}
	_, settingsErr := c.SafeSettings()
	_, aliasErr := c.Aliases()
	if settingsErr == nil || aliasErr == nil {
		t.Fatalf("expected errors for recursive includes, got %v and %v", settingsErr, aliasErr)
	}
	if settingsErr.Error() != aliasErr.Error() {
		t.Fatalf("SafeSettings error %q differs from Aliases error %q", settingsErr, aliasErr)
	}
}

func TestExpandIncludePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	sshDir := filepath.Join("base", ".ssh")
	abs, _ := filepath.Abs(filepath.Join("x", "y"))
	tests := []struct{ in, want string }{
		{"config.d/*", filepath.Join(sshDir, "config.d", "*")},
		{"~/other", filepath.Join(home, "other")},
		{abs, abs},
	}
	for _, tt := range tests {
		if got := expandIncludePath(tt.in, sshDir); got != tt.want {
			t.Errorf("expandIncludePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConnectArgs(t *testing.T) {
	tests := []struct {
		key    string
		claude bool
		want   []string
	}{
		{"", false, []string{"ssh", "web"}},
		{"/k/id", false, []string{"ssh", "-i", "/k/id", "web"}},
		{"", true, []string{"ssh", "-t", "web", "claude"}},
		{"/k/id", true, []string{"ssh", "-t", "-i", "/k/id", "web", "claude"}},
	}
	for _, tt := range tests {
		if got := ConnectArgs("web", tt.key, tt.claude); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ConnectArgs(web, %q, %v) = %v, want %v", tt.key, tt.claude, got, tt.want)
		}
	}
}
