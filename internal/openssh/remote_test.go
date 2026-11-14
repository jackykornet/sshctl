package openssh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// fakeRemote is a stand-in for ssh: sh runs on POSIX, cmd on Windows, in
// place of the whole ssh command line, which is recorded in argv.
type fakeRemote struct{ sh, cmd string }

func (f fakeRemote) client(argv *[]string) *Client {
	return &Client{Exec: func(ctx context.Context, args []string) *exec.Cmd {
		*argv = args
		if runtime.GOOS == "windows" {
			return exec.CommandContext(ctx, "cmd.exe", "/d", "/q", "/s", "/c", f.cmd)
		}
		return exec.CommandContext(ctx, "sh", "-c", f.sh)
	}}
}

func TestRunPassesOutputWithoutTheShellMarker(t *testing.T) {
	// The fake host reads the script and answers as POSIX sh would.
	var argv []string
	c := fakeRemote{
		sh:  `cat >/dev/null; echo motd; echo ` + posixMarker + `; echo hello`,
		cmd: `echo motd& echo ` + posixMarker + `& echo hello`,
	}.client(&argv)
	var out bytes.Buffer
	if err := c.Run(context.Background(), "web", RemoteOptions{Identity: "/k/id"}, "echo hello", &out, &out); err != nil {
		t.Fatal(err)
	}
	if got := normalize(out.String()); got != "motd\nhello\n" {
		t.Errorf("output = %q (raw %q), want motd and hello without the marker", got, out.String())
	}
	if want := []string{"ssh", "-i", "/k/id", "web", "sh", "-s"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestRunReportsAHostWithoutPOSIXShell(t *testing.T) {
	var argv []string
	c := fakeRemote{
		sh:  `echo "sh: command not found" >&2; exit 127`,
		cmd: `echo 'sh' is not recognized as an internal or external command 1>&2& exit /b 1`,
	}.client(&argv)
	var out bytes.Buffer
	err := c.Run(context.Background(), "win", RemoteOptions{}, "true", &out, &out)
	if !errors.Is(err, ErrNoPOSIXShell) {
		t.Fatalf("err = %v, want ErrNoPOSIXShell", err)
	}
}

func TestRunReportsAFailedConnectionAsSuch(t *testing.T) {
	var argv []string
	c := fakeRemote{
		sh:  `echo "Permission denied (publickey)." >&2; exit 255`,
		cmd: `echo Permission denied (publickey).1>&2& exit /b 255`,
	}.client(&argv)
	var out bytes.Buffer
	err := c.Run(context.Background(), "web", RemoteOptions{Identity: "/k/id", KeyOnly: true}, "", &out, &out)
	if err == nil || errors.Is(err, ErrNoPOSIXShell) {
		t.Fatalf("err = %v, want a connection failure", err)
	}
	if !ConnectFailed(err) {
		t.Errorf("ConnectFailed(%v) = false, want true", err)
	}
	want := []string{"ssh", "-i", "/k/id", "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no", "-o", "PreferredAuthentications=publickey", "-o", "IdentitiesOnly=yes",
		"web", "sh", "-s"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q\nwant %q", argv, want)
	}
}

// normalize turns Windows line endings into "\n".
func normalize(s string) string {
	return string(bytes.ReplaceAll([]byte(s), []byte("\r\n"), []byte("\n")))
}

// localHost is a Client whose ssh runs the script with the local sh, in a
// fresh home directory standing in for the remote user's.
func localHost(t *testing.T) (*Client, string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to stand in for the host")
	}
	home := t.TempDir()
	return &Client{Exec: func(ctx context.Context, argv []string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, sh, "-s")
		cmd.Env = append(os.Environ(), "HOME="+home)
		return cmd
	}}, home
}

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJ0ZXN0a2V5 jo@laptop"

func TestInstallKeyAddsTheKeyOnceWithSafePermissions(t *testing.T) {
	c, home := localHost(t)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o755); err != nil {
		t.Fatal(err)
	}
	// An existing file without a final newline must not run into the key.
	other := "ssh-rsa AAAAB3NzaC1yc2E other@box"
	if err := os.WriteFile(filepath.Join(ssh, "authorized_keys"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	added, err := c.InstallKey(context.Background(), "web", "", testKey, &errOut)
	if err != nil || !added {
		t.Fatalf("first install: added = %v, err = %v (%s)", added, err, errOut.String())
	}
	// The same key with another comment is the same key.
	added, err = c.InstallKey(context.Background(), "web", "", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJ0ZXN0a2V5 renamed", &errOut)
	if err != nil || added {
		t.Fatalf("second install: added = %v, err = %v, want already there", added, err)
	}
	b, err := os.ReadFile(filepath.Join(ssh, "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if want := other + "\n" + testKey + "\n"; string(b) != want {
		t.Errorf("authorized_keys = %q, want %q", b, want)
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{ssh: 0o700, filepath.Join(ssh, "authorized_keys"): 0o600} {
			if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != want {
				t.Errorf("%s mode = %v (%v), want %v", path, fi.Mode().Perm(), err, want)
			}
		}
	}
}

func TestInstallKeyCreatesTheSSHDirectory(t *testing.T) {
	c, home := localHost(t)
	if added, err := c.InstallKey(context.Background(), "web", "", testKey, nil); err != nil || !added {
		t.Fatalf("added = %v, err = %v", added, err)
	}
	if b, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys")); err != nil || string(b) != testKey+"\n" {
		t.Errorf("authorized_keys = %q, %v", b, err)
	}
}

func TestInstallKeyRefusesWhatIsNotAPublicKey(t *testing.T) {
	c := &Client{Exec: func(context.Context, []string) *exec.Cmd {
		t.Fatal("ssh must not start")
		return nil
	}}
	for _, key := range []string{"", "ssh-ed25519", "-----BEGIN OPENSSH PRIVATE KEY-----", "ssh-ed25519 AAAA'; rm -rf ~ #"} {
		if _, err := c.InstallKey(context.Background(), "web", "", key, nil); err == nil {
			t.Errorf("InstallKey(%q) succeeded, want an error", key)
		}
	}
}
