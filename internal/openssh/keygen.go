package openssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// keyNames are the files GenerateKey may create in ~/.ssh, in order.
var keyNames = []string{"id_ed25519", "sshctl_ed25519"}

// GenerateKey creates a new ed25519 key pair in sshDir (the user's ~/.ssh,
// created private if missing) and returns its private key path:
// id_ed25519, or sshctl_ed25519 when id_ed25519 or its .pub already exists.
// It never overwrites a key, and refuses when both names are taken.
//
// run starts "ssh-keygen" on the terminal (out is nil), so ssh-keygen asks
// the user for a passphrase, which may be empty. Tests replace run.
func GenerateKey(ctx context.Context, run Runner, sshDir string) (string, error) {
	path, err := freeKeyPath(sshDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return "", err
	}
	if err := run(ctx, []string{"ssh-keygen", "-t", "ed25519", "-f", path}, nil); err != nil {
		return "", fmt.Errorf("ssh-keygen: %w", err)
	}
	for _, f := range []string{path, path + ".pub"} {
		if _, err := os.Stat(f); err != nil {
			return "", fmt.Errorf("ssh-keygen did not write %s: %w", f, err)
		}
	}
	return path, nil
}

// freeKeyPath is the first of keyNames in sshDir with neither the key nor
// its .pub present. A dangling symlink counts as present.
func freeKeyPath(sshDir string) (string, error) {
	for _, name := range keyNames {
		path := filepath.Join(sshDir, name)
		taken := false
		for _, f := range []string{path, path + ".pub"} {
			_, err := os.Lstat(f)
			switch {
			case err == nil:
				taken = true
			case !errors.Is(err, fs.ErrNotExist):
				return "", err
			}
		}
		if !taken {
			return path, nil
		}
	}
	return "", fmt.Errorf("will not overwrite an existing key: %s and %s are both taken in %s; choose a key with: sshctl key use <private-key-path>", keyNames[0], keyNames[1], sshDir)
}
