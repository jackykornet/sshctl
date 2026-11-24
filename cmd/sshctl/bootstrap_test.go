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

// newKey is the public key the fakeHost's ssh-keygen writes.
const (
	newKey     = "ssh-ed25519 " + newKeyBlob + " jo@laptop"
	newKeyBlob = "AAAAC3NzaC1lZDI1NTE5AAAAIG5ld2tleTAx"
)

// fakeHost is an openssh.Runner playing ssh and the host behind it. It
// keeps the installed key blobs, logs every call ("resolve", "install",
// "key-only", "identities") with its argv, and answers like a POSIX host
// unless noPOSIX. With reject the key never logs in on its own. "ssh -G -i"
// lists the key and otherIdentities as identity files. onKeyOnly, when set,
// runs at the key-only check.
//
// It also plays ssh-keygen ("keygen"): it writes newKey's pair at the -f
// path, unless keygenFails.
type fakeHost struct {
	keys        []string
	noPOSIX     bool
	reject      bool
	keygenFails bool
	// otherIdentities are the identity files ssh_config adds for the host.
	otherIdentities []string
	onKeyOnly       func()
	calls           []string
	argv            [][]string
}

var installedBlob = regexp.MustCompile(`k='\\''([A-Za-z0-9+/=]+)'\\''`)

func (h *fakeHost) run(_ context.Context, argv []string, out io.Writer) error {
	h.argv = append(h.argv, argv)
	remote := argv[len(argv)-1]
	switch {
	case argv[0] == "ssh-keygen":
		h.calls = append(h.calls, "keygen")
		if h.keygenFails {
			return exitCodeError(1)
		}
		path := argv[slices.Index(argv, "-f")+1]
		if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
			return err
		}
		return os.WriteFile(path+".pub", []byte(newKey+"\n"), 0o644)
	case slices.Contains(argv, "-G") && slices.Contains(argv, "-i"):
		h.calls = append(h.calls, "identities")
		fmt.Fprintf(out, "host web\nidentityfile %s\n", argv[slices.Index(argv, "-i")+1])
		for _, f := range h.otherIdentities {
			fmt.Fprintf(out, "identityfile %s\n", f)
		}
		return nil
	case slices.Contains(argv, "-G"):
		h.calls = append(h.calls, "resolve")
		fmt.Fprint(out, "host web\nhostname 10.0.0.5\nuser deploy\nport 22\n")
		return nil
	case slices.Contains(argv, "PreferredAuthentications=publickey"):
		h.calls = append(h.calls, "key-only")
		if h.onKeyOnly != nil {
			h.onKeyOnly()
		}
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
	cfg.SelectedIdentity = key
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
	if want := []string{"resolve", "install", "key-only", "identities"}; !slices.Equal(h.calls, want) {
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

func TestBootstrapClearsAnOldIdentityFileBeforeCheckingTheKey(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5", IdentityFile: "~/.ssh/old"})
	during := "unchecked"
	h.onKeyOnly = func() { during = hostBlock(t, a, "web").IdentityFile }
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	if during != "" {
		t.Errorf("IdentityFile during the key-only check = %q, want none", during)
	}
	if got := hostBlock(t, a, "web").IdentityFile; got != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/id_ed25519", got)
	}
}

func TestBootstrapRestoresTheOldIdentityFileWhenTheKeyAloneFails(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5", IdentityFile: "~/.ssh/old"})
	h.reject = true
	err := a.execute([]string{"bootstrap", "web"})
	if err == nil || !strings.Contains(err.Error(), "without a password") || !strings.Contains(err.Error(), "IdentityFile was not set") {
		t.Fatalf("err = %v, want why the key-only check failed\n%s", err, out)
	}
	// Step 1 stays; the old IdentityFile is put back.
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
	if want := []string{"install", "key-only", "identities"}; !slices.Equal(h.calls, want) {
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

func TestBootstrapRestoresTheOldIdentityFileWhenInstallingFails(t *testing.T) {
	a, _, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5", IdentityFile: "~/.ssh/old"})
	h.noPOSIX = true
	if err := a.execute([]string{"bootstrap", "web"}); err == nil {
		t.Fatal("bootstrap succeeded on a host without POSIX sh")
	}
	if got := hostBlock(t, a, "web").IdentityFile; got != "~/.ssh/old" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/old back", got)
	}
}

func TestBootstrapNotesIdentityFilesSSHConfigAlsoOffers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		others []string
		note   bool
	}{
		{"none", nil, false},
		{"one", []string{"~/.ssh/old"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, out, h := bootstrapTree(t, "Host web\n  HostName 10.0.0.5\n")
			h.otherIdentities = tc.others
			if err := a.execute([]string{"bootstrap", "web"}); err != nil {
				t.Fatalf("bootstrap: %v\n%s", err, out)
			}
			note := "ssh also offered ~/.ssh/old"
			if got := strings.Contains(out.String(), note); got != tc.note {
				t.Errorf("output has %q: %v, want %v\n%s", note, got, tc.note, out)
			}
		})
	}
}

func TestBootstrapRefusesAnUnknownAliasBeforeRunningAnything(t *testing.T) {
	for _, tc := range []struct {
		name       string
		noIdentity bool
	}{
		{"with a Selected identity", false},
		{"before generating a key", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, h := bootstrapTree(t, openssh.IncludeLine+"\n")
			managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
			if tc.noIdentity {
				withoutIdentity(t, a)
			}
			err := a.execute([]string{"bootstrap", "nope"})
			if err == nil || !strings.Contains(err.Error(), "not a Host in ssh_config") {
				t.Errorf("err = %v, want the unknown alias", err)
			}
			if len(h.calls) != 0 {
				t.Errorf("ran %q, want nothing", h.calls)
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

// withoutIdentity clears a's Selected identity and removes the key
// bootstrapTree put in ~/.ssh, so ~/.ssh holds no key.
func withoutIdentity(t *testing.T, a *app) {
	t.Helper()
	if err := a.catalog().SetIdentity(""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(a.ssh.Home, ".ssh", "id_ed25519.pub")); err != nil {
		t.Fatal(err)
	}
}

func selectedIdentity(t *testing.T, a *app) string {
	t.Helper()
	snap, err := a.catalog().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap.Identity
}

func TestBootstrapGeneratesAndSelectsAKeyWhenThereIsNoSelectedIdentity(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	withoutIdentity(t, a)
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	if want := []string{"keygen", "resolve", "install", "key-only", "identities"}; !slices.Equal(h.calls, want) {
		t.Errorf("calls = %q, want %q", h.calls, want)
	}
	key := filepath.Join(a.ssh.Home, ".ssh", "id_ed25519")
	if got := selectedIdentity(t, a); got != key {
		t.Errorf("Selected identity = %q, want %q", got, key)
	}
	if !slices.Equal(h.keys, []string{newKeyBlob}) {
		t.Errorf("installed keys = %q, want the new key", h.keys)
	}
	if got := hostBlock(t, a, "web").IdentityFile; got != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/id_ed25519", got)
	}
	if !strings.Contains(out.String(), "~/.ssh/id_ed25519 is now the Selected identity") {
		t.Errorf("output does not say the new key is selected:\n%s", out)
	}
}

func TestBootstrapNeverOverwritesAnUnselectedIDEd25519(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	if err := a.catalog().SetIdentity(""); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(a.ssh.Home, ".ssh", "id_ed25519.pub")
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(old); string(b) != bootstrapKey+"\n" {
		t.Errorf("id_ed25519.pub = %q, want it untouched", b)
	}
	if got, want := selectedIdentity(t, a), filepath.Join(a.ssh.Home, ".ssh", "sshctl_ed25519"); got != want {
		t.Errorf("Selected identity = %q, want %q", got, want)
	}
	if !slices.Equal(h.keys, []string{newKeyBlob}) {
		t.Errorf("installed keys = %q, want the new key", h.keys)
	}
	if got := hostBlock(t, a, "web").IdentityFile; got != "~/.ssh/sshctl_ed25519" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/sshctl_ed25519", got)
	}
}

func TestBootstrapStopsWhenKeyGenerationFails(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	withoutIdentity(t, a)
	h.keygenFails = true
	err := a.execute([]string{"bootstrap", "web"})
	if err == nil || !strings.Contains(err.Error(), "ssh-keygen") {
		t.Fatalf("err = %v, want the ssh-keygen failure\n%s", err, out)
	}
	if want := []string{"keygen"}; !slices.Equal(h.calls, want) {
		t.Errorf("calls = %q, want %q", h.calls, want)
	}
	if got := selectedIdentity(t, a); got != "" {
		t.Errorf("Selected identity = %q, want none", got)
	}
}

func TestBootstrapUsesTheSpacesSelectedIdentity(t *testing.T) {
	a, out, h := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	key := filepath.Join(a.ssh.Home, ".ssh", "id_ed25519")
	cat := a.catalog()
	for _, err := range []error{
		cat.AddSpace("acme"),
		cat.SetSpace("web", "acme"),
		cat.SetSpaceIdentity("acme", key),
		cat.SetIdentity(filepath.Join(a.ssh.Home, ".ssh", "id_other")), // has no .pub
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	for _, argv := range h.argv[1:] {
		if !slices.Contains(argv, key) {
			t.Errorf("ssh %q does not use acme's Selected identity", argv)
		}
	}
}

// Bootstrap rewrites the Host block through catalog.Save, which replaces
// Tunnels: it keeps the Connection's own.
func TestBootstrapKeepsTunnels(t *testing.T) {
	a, out, _ := bootstrapTree(t, openssh.IncludeLine+"\n")
	managedWeb(t, a, openssh.Endpoint{HostName: "10.0.0.5"})
	if err := a.execute([]string{"tunnel", "add", "web", "panel=8083"}); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"bootstrap", "web"}); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	if got := load(t, a).Connections["web"].Tunnels; len(got) != 1 || got[0].Name != "panel" {
		t.Errorf("Tunnels = %+v, want panel kept", got)
	}
}
