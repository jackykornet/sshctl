package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

const bootstrapKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJ0ZXN0a2V5 jo@laptop"

// fakeHost is an openssh.Runner playing ssh and the host behind it. It
// keeps the installed key blobs, logs every call ("resolve", "install",
// "key-only") with its argv, and answers like a POSIX host unless noPOSIX.
// With reject the key never logs in on its own.
type fakeHost struct {
	keys    []string
	noPOSIX bool
	reject  bool
	calls   []string
	argv    [][]string
}

var installedBlob = regexp.MustCompile(`k='\\''([A-Za-z0-9+/=]+)'\\''`)

func (h *fakeHost) run(_ context.Context, argv []string, out io.Writer) error {
	h.argv = append(h.argv, argv)
	remote := argv[len(argv)-1]
	switch {
	case slices.Contains(argv, "-G"):
		h.calls = append(h.calls, "resolve")
		fmt.Fprint(out, "host web\nhostname 10.0.0.5\nuser deploy\nport 22\n")
		return nil
	case slices.Contains(argv, "BatchMode=yes"):
		h.calls = append(h.calls, "key-only")
		if len(h.keys) == 0 || h.reject {
			return exitCodeError(255)
		}
		return nil
	}
	h.calls = append(h.calls, "install")
	if h.noPOSIX {
		return exitCodeError(127)
	}
	fmt.Fprintln(out, "sshctl-posix-2")
	blob := installedBlob.FindStringSubmatch(remote)[1]
	if slices.Contains(h.keys, blob) {
		fmt.Fprintln(out, "sshctl-key-present")
		return nil
	}
	h.keys = append(h.keys, blob)
	fmt.Fprintln(out, "sshctl-key-added")
	return nil
}

// bootstrapTree is an app over sshConfig with a Selected identity
// ~/.ssh/id_ed25519 in a fake home and ssh replaced by a fakeHost.
func bootstrapTree(t *testing.T, sshConfig string) (*app, *strings.Builder, *fakeHost) {
	t.Helper()
	home := t.TempDir()
	key := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key+".pub", []byte(bootstrapKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.DefaultKey = key
	a, _ := cliTree(t, sshConfig, cfg, "")
	var out strings.Builder
	a.out, a.err = &out, &out
	a.ssh.Home = home
	h := &fakeHost{}
	a.runSSH = h.run
	return a, &out, h
}

// managedWeb gives a a Managed connection web with only a HostName.
func managedWeb(t *testing.T, a *app, e openssh.Endpoint) {
	t.Helper()
	if err := a.ssh.SaveHost("", openssh.Host{Alias: "web", Endpoint: e}); err != nil {
		t.Fatal(err)
	}
}

func hostBlock(t *testing.T, a *app, alias string) openssh.Endpoint {
	t.Helper()
	hosts, err := a.ssh.ManagedHosts()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hosts {
		if h.Alias == alias {
			return h.Endpoint
		}
	}
	t.Fatalf("no Host block for %s", alias)
	return openssh.Endpoint{}
}

func TestBootstrapInstallsTheKeyThenPointsTheHostBlockAtIt(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	if want := []string{"resolve", "install", "key-only"}; !slices.Equal(h.calls, want) {
		t.Errorf("ssh calls = %q, want %q", h.calls, want)
	}
	key := filepath.Join(a.ssh.Home, ".ssh", "id_ed25519")
	for _, argv := range h.argv[1:] {
		if !slices.Contains(argv, key) {
			t.Errorf("ssh %q does not use the Selected identity", argv)
		}
	}
	want := openssh.Endpoint{HostName: "10.0.0.5", User: "deploy", Port: "22", IdentityFile: "~/.ssh/id_ed25519"}
	if got := hostBlock(t, a, "web"); got != want {
		t.Errorf("Host block = %+v, want %+v", got, want)
	}
	for _, s := range []string{"User deploy", "Port 22", "added", "IdentityFile ~/.ssh/id_ed25519"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
}

func TestBootstrapCanBeRunAgain(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	for range 2 {
		if err := a.execute([]string{"bootstrap", "web"}); err != nil {
			t.Fatalf("bootstrap: %v\n%s", err, out)
		}
	}
	if len(h.keys) != 1 {
		t.Errorf("installed keys = %q, want the key once", h.keys)
	}
	if !strings.Contains(out.String(), "already") || !strings.Contains(out.String(), "already has HostName, User and Port") {
		t.Errorf("second run should say the key and endpoint are already there:\n%s", out)
	}
	if got := hostBlock(t, a, "web").IdentityFile; got != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile = %q", got)
	}
}

func TestBootstrapLeavesIdentityFileUnsetWhenTheKeyAloneFails(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5", IdentityFile: "~/.ssh/old"})
	h.reject = true
	err := a.execute([]string{"bootstrap", "web"})
	if err == nil || !strings.Contains(err.Error(), "without a password") || !strings.Contains(err.Error(), "IdentityFile was not set") {
		t.Fatalf("err = %v, want why the key-only check failed\n%s", err, out)
	}
	// Step 1 stays; the old IdentityFile is untouched.
	want := openssh.Endpoint{HostName: "10.0.0.5", User: "deploy", Port: "22", IdentityFile: "~/.ssh/old"}
	if got := hostBlock(t, a, "web"); got != want {
		t.Errorf("Host block = %+v, want %+v", got, want)
	}
}

func TestBootstrapOnlyInstallsTheKeyOnAConnectionThatIsNotManaged(t *testing.T) {
	sshConfig := "Host web\n  HostName 10.0.0.5\n"
	a, out, h := bootstrapTree(t, sshConfig)
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	if want := []string{"install", "key-only"}; !slices.Equal(h.calls, want) {
		t.Errorf("ssh calls = %q, want %q", h.calls, want)
	}
	if b, _ := os.ReadFile(a.ssh.SSHConfig); string(b) != sshConfig {
		t.Errorf("ssh_config = %q, want it unchanged", b)
	}
	if hosts, _ := a.ssh.ManagedHosts(); len(hosts) != 0 {
		t.Errorf("managed hosts = %+v, want none", hosts)
	}
	if !strings.Contains(out.String(), "left as it is") {
		t.Errorf("output lacks the note that the Host block was left alone:\n%s", out)
	}
}

func TestBootstrapRefusesBeforeRunningSSH(t *testing.T) {
	for _, tc := range []struct {
		name, alias, want string
		noIdentity        bool
	}{
		{"no Selected identity", "web", "no Selected identity", true},
		{"unknown alias", "nope", "not a Host in ssh_config", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, h := bootstrapTree(t, openssh.IncludeLine+"\n")
			managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
			if tc.noIdentity {
				c := load(t, a)
				c.DefaultKey = ""
				if err := config.Save(a.cfgPath, c); err != nil {
					t.Fatal(err)
				}
			}
			err := a.execute([]string{"bootstrap", tc.alias})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if len(h.calls) != 0 {
				t.Errorf("ssh ran %q, want nothing", h.calls)
			}
		})
	}
}

func TestBootstrapFailsEarlyWithoutPOSIXSh(t *testing.T) {
	a, _, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	h.noPOSIX = true
	err := a.execute([]string{"bootstrap", "web"})
	if err == nil || !strings.Contains(err.Error(), "POSIX sh") {
		t.Fatalf("err = %v, want the missing POSIX sh", err)
	}
	if slices.Contains(h.calls, "key-only") || hostBlock(t, a, "web").IdentityFile != "" {
		t.Errorf("calls %q, Host block %+v: want nothing after the install", h.calls, hostBlock(t, a, "web"))
	}
}
