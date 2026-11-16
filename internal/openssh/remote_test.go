package openssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestRemoteArgsRunsTheScriptWithSh(t *testing.T) {
	for _, tc := range []struct {
		key, script string
		want        []string
	}{
		{"", "echo hi", []string{"ssh", "web", "sh -c 'echo hi'"}},
		{"/k/id", "echo 'it'", []string{"ssh", "-i", "/k/id", "web", `sh -c 'echo '\''it'\'''`}},
	} {
		if got := RemoteArgs("web", tc.key, tc.script); !slices.Equal(got, tc.want) {
			t.Errorf("RemoteArgs(%q) = %q, want %q", tc.script, got, tc.want)
		}
	}
}

// TestRemoteArgsQuotingSurvivesALoginShell runs the command ssh would hand
// the remote login shell through a local sh.
func TestRemoteArgsQuotingSurvivesALoginShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}
	argv := RemoteArgs("web", "", `printf '%s|' "a b" 'c'"'"'d' $((1+1))`)
	out, err := exec.Command("sh", "-c", argv[len(argv)-1]).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "a b|c'd|2|"; got != want {
		t.Errorf("remote output = %q, want %q", got, want)
	}
}

// fakeSSH is a Runner that records argv and answers with output and err.
type fakeSSH struct {
	calls  [][]string
	output string
	err    error
}

func (f *fakeSSH) run(_ context.Context, argv []string, out io.Writer) error {
	f.calls = append(f.calls, argv)
	if out != nil {
		fmt.Fprint(out, f.output)
	}
	return f.err
}

type exitErr int

func (e exitErr) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

func TestCheckPOSIX(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  string
		err     error
		wantErr error
	}{
		{"sh answers", "sshctl-posix-2\n", nil, nil},
		{"no sh", "sh: command not found\n", exitErr(127), ErrNoPOSIX},
		{"not POSIX", "sshctl-posix-$((1+1))\n", nil, ErrNoPOSIX},
		{"ssh fails", "", exitErr(255), exitErr(255)},
		{"no ssh", "", exec.ErrNotFound, exec.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSSH{output: tc.output, err: tc.err}
			err := CheckPOSIX(context.Background(), f.run, "web", "/k/id")
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			want := []string{"ssh", "-i", "/k/id", "web", "sh -c 'echo sshctl-posix-$((1+1))'"}
			if len(f.calls) != 1 || !slices.Equal(f.calls[0], want) {
				t.Errorf("ran %q, want %q", f.calls, want)
			}
		})
	}
}

// localHost is a Runner that hands the remote command, as ssh would hand it
// to the login shell, to a local sh in a fresh home standing in for the
// remote user's. It returns that home.
func localHost(t *testing.T) (Runner, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the script runs on a POSIX host")
	}
	home := t.TempDir()
	return func(ctx context.Context, argv []string, out io.Writer) error {
		cmd := exec.CommandContext(ctx, "sh", "-c", argv[len(argv)-1])
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdout, cmd.Stderr = out, os.Stderr
		return cmd.Run()
	}, home
}

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJ0ZXN0a2V5 jo@laptop"

func TestInstallKeyAddsTheKeyOnceWithSafePermissions(t *testing.T) {
	run, home := localHost(t)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o755); err != nil {
		t.Fatal(err)
	}
	// An existing file without a final newline must not run into the key,
	// and a longer blob that starts like ours is another key.
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJ0ZXN0a2V5more other@box"
	keys := filepath.Join(ssh, "authorized_keys")
	if err := os.WriteFile(keys, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := InstallKey(context.Background(), run, "web", "/k/id", testKey)
	if err != nil || !added {
		t.Fatalf("first install: added = %v, err = %v", added, err)
	}
	// The same key with another comment is the same key.
	added, err = InstallKey(context.Background(), run, "web", "/k/id", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJ0ZXN0a2V5 renamed")
	if err != nil || added {
		t.Fatalf("second install: added = %v, err = %v, want already there", added, err)
	}
	b, err := os.ReadFile(keys)
	if err != nil {
		t.Fatal(err)
	}
	if want := other + "\n" + testKey + "\n"; string(b) != want {
		t.Errorf("authorized_keys = %q, want %q", b, want)
	}
	for path, want := range map[string]os.FileMode{ssh: 0o700, keys: 0o600} {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v (%v), want %v", path, fi.Mode().Perm(), err, want)
		}
	}
}

func TestInstallKeyCreatesTheSSHDirectory(t *testing.T) {
	run, home := localHost(t)
	if added, err := InstallKey(context.Background(), run, "web", "", testKey); err != nil || !added {
		t.Fatalf("added = %v, err = %v", added, err)
	}
	if b, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys")); err != nil || string(b) != testKey+"\n" {
		t.Errorf("authorized_keys = %q, %v", b, err)
	}
}

func TestInstallKeyLogsInWithTheSelectedIdentity(t *testing.T) {
	f := &fakeSSH{output: "sshctl-posix-2\nsshctl-key-added\n"}
	if _, err := InstallKey(context.Background(), f.run, "web", "/k/id", testKey); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || !slices.Equal(f.calls[0][:4], []string{"ssh", "-i", "/k/id", "web"}) {
		t.Errorf("ran %q, want one ssh -i /k/id web", f.calls)
	}
}

func TestInstallKeyFailsWithoutChangingAnything(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  string
		err     error
		wantErr error
	}{
		{"no sh", "sh: command not found\n", exitErr(127), ErrNoPOSIX},
		{"ssh fails", "", exitErr(255), exitErr(255)},
		{"script fails", "sshctl-posix-2\n", exitErr(1), exitErr(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSSH{output: tc.output, err: tc.err}
			added, err := InstallKey(context.Background(), f.run, "web", "", testKey)
			if added || !errors.Is(err, tc.wantErr) {
				t.Errorf("added = %v, err = %v, want %v", added, err, tc.wantErr)
			}
		})
	}
}

func TestInstallKeyRefusesWhatIsNotAPublicKey(t *testing.T) {
	f := &fakeSSH{}
	for _, key := range []string{"", "ssh-ed25519", "-----BEGIN OPENSSH PRIVATE KEY-----", "ssh-ed25519 AAAA'; rm -rf ~ #"} {
		if _, err := InstallKey(context.Background(), f.run, "web", "", key); err == nil {
			t.Errorf("InstallKey(%q) succeeded, want an error", key)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("ssh ran %q, want nothing", f.calls)
	}
}

func TestCheckKeyOnlyAllowsNothingButTheKey(t *testing.T) {
	f := &fakeSSH{err: exitErr(255)}
	if err := CheckKeyOnly(context.Background(), f.run, "web", "/k/id"); !errors.Is(err, exitErr(255)) {
		t.Errorf("err = %v, want ssh's failure", err)
	}
	// No BatchMode, so ssh can ask for the key's passphrase; nothing but
	// that public key may log in.
	want := []string{"ssh", "-i", "/k/id", "-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no", "-o", "PreferredAuthentications=publickey", "-o", "IdentitiesOnly=yes",
		"web", "exit 0"}
	if len(f.calls) != 1 || !slices.Equal(f.calls[0], want) {
		t.Errorf("ran %q\nwant %q", f.calls, want)
	}
}

func TestOtherIdentitiesListsWhatSSHConfigAddsToTheKey(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		want         []string
	}{
		{"only the key", "host web\nidentityfile /home/jo/.ssh/id\n", nil},
		{"the key as ~/", "identityfile ~/.ssh/id\n", nil},
		{"another key", "identityfile /home/jo/.ssh/id\nidentityfile ~/.ssh/old\nidentitiesonly yes\n", []string{"~/.ssh/old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSSH{output: tc.output}
			got, err := OtherIdentities(context.Background(), f.run, "web", "/home/jo/.ssh/id", "/home/jo")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("OtherIdentities = %q, want %q", got, tc.want)
			}
			if want := []string{"ssh", "-G", "-i", "/home/jo/.ssh/id", "web"}; len(f.calls) != 1 || !slices.Equal(f.calls[0], want) {
				t.Errorf("ran %q, want %q", f.calls, want)
			}
		})
	}
}

func TestResolveReadsTheEndpointFromSSH(t *testing.T) {
	f := &fakeSSH{output: "host web\nhostname 10.0.0.5\nuser deploy\nport 2222\nidentityfile ~/.ssh/id_rsa\n"}
	got, err := Resolve(context.Background(), f.run, "web")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Endpoint{HostName: "10.0.0.5", User: "deploy", Port: "2222"}); got != want {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}
	if want := []string{"ssh", "-G", "web"}; len(f.calls) != 1 || !slices.Equal(f.calls[0], want) {
		t.Errorf("ran %q, want %q", f.calls, want)
	}
}
