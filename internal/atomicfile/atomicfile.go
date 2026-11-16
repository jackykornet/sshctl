// Package atomicfile replaces files so that an interrupted write never
// leaves one truncated, writing through a symlink rather than over it.
// sshctl's config, state.json and ssh_config files are all saved with it.
package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces path with data through a temporary file in the same
// directory, creating path's directory (0700) when it is missing. When path
// is a symlink (chezmoi can link it into its source directory, see
// ADR-0004) the write goes to the symlink's target and the symlink stays; a
// symlink to nothing is an error, so it is never replaced by a plain file.
func Write(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	path, err := resolveSymlink(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sshctl-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// resolveSymlink returns the file a write to path should replace: path
// itself, or the final target when path is a symlink.
func resolveSymlink(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		dest, _ := os.Readlink(path)
		return "", fmt.Errorf("%s is a symlink to %s, which does not exist", path, dest)
	}
	return target, err
}
