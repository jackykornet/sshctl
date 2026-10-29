package openssh

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAliasesIgnoresPatternsAndDuplicates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte("Host web api\nHost *.example.com !bad\nHost api db # repeated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := HostAliases(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "api", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
