package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCreatesTheDirectoryAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "dir", "file")
	if err := Write(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "data" {
		t.Fatalf("file = %q, %v", b, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory holds %v, want only the file (no temporary file left)", entries)
	}
}

func TestWriteGoesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot make symlinks here: %v", err)
	}
	if err := Write(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v, %v", fi, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("target = %q, want new", b)
	}
}

func TestWriteRefusesADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "gone", "target"), link); err != nil {
		t.Skipf("cannot make symlinks here: %v", err)
	}
	err := Write(link, []byte("new"), 0o600)
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), link) {
		t.Fatalf("err = %v, want a clear error naming the link", err)
	}
	if fi, _ := os.Lstat(link); fi == nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling link was replaced")
	}
}
