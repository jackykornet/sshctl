package openssh

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeKeygen is a Runner playing ssh-keygen: it writes the key pair at the
// -f path unless skip, and logs every argv.
type fakeKeygen struct {
	argv [][]string
	skip bool
}

func (k *fakeKeygen) run(_ context.Context, argv []string, _ io.Writer) error {
	k.argv = append(k.argv, argv)
	if k.skip {
		return nil
	}
	path := argv[slices.Index(argv, "-f")+1]
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(path+".pub", []byte("ssh-ed25519 AAAA new"), 0o644)
}

func TestGenerateKeyNeverOverwritesAKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		noSSHDir bool
		existing []string
		want     string
	}{
		{"no keys", false, nil, "id_ed25519"},
		{"no .ssh directory", true, nil, "id_ed25519"},
		{"id_ed25519 taken", false, []string{"id_ed25519"}, "sshctl_ed25519"},
		{"only its .pub left", false, []string{"id_ed25519.pub"}, "sshctl_ed25519"},
		{"other keys only", false, []string{"id_rsa", "sshctl_ed25519.pub"}, "id_ed25519"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sshDir := filepath.Join(t.TempDir(), ".ssh")
			if !tc.noSSHDir {
				if err := os.Mkdir(sshDir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range tc.existing {
				if err := os.WriteFile(filepath.Join(sshDir, f), []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			k := &fakeKeygen{}
			got, err := GenerateKey(context.Background(), k.run, sshDir)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(sshDir, tc.want)
			if got != want {
				t.Errorf("key = %q, want %q", got, want)
			}
			if wantArgv := []string{"ssh-keygen", "-t", "ed25519", "-f", want}; len(k.argv) != 1 || !slices.Equal(k.argv[0], wantArgv) {
				t.Errorf("ran %q, want %q", k.argv, wantArgv)
			}
			for _, f := range tc.existing {
				if b, err := os.ReadFile(filepath.Join(sshDir, f)); err != nil || string(b) != "old" {
					t.Errorf("%s = %q, %v; want it untouched", f, b, err)
				}
			}
		})
	}
}

func TestGenerateKeyRefusesWhenBothNamesAreTaken(t *testing.T) {
	sshDir := t.TempDir()
	for _, f := range []string{"id_ed25519", "sshctl_ed25519"} {
		if err := os.WriteFile(filepath.Join(sshDir, f), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	k := &fakeKeygen{}
	_, err := GenerateKey(context.Background(), k.run, sshDir)
	if err == nil || !strings.Contains(err.Error(), "sshctl_ed25519") {
		t.Fatalf("err = %v, want a refusal naming the taken files", err)
	}
	if len(k.argv) != 0 {
		t.Errorf("ran %q, want nothing", k.argv)
	}
}

func TestGenerateKeyFailsWhenNoKeyWasWritten(t *testing.T) {
	sshDir := t.TempDir()
	k := &fakeKeygen{skip: true}
	if _, err := GenerateKey(context.Background(), k.run, sshDir); err == nil {
		t.Fatal("want an error when ssh-keygen wrote no key")
	}
	failing := func(context.Context, []string, io.Writer) error { return errors.New("exit status 1") }
	if _, err := GenerateKey(context.Background(), failing, sshDir); err == nil || !strings.Contains(err.Error(), "ssh-keygen") {
		t.Fatalf("err = %v, want the ssh-keygen failure", err)
	}
}
