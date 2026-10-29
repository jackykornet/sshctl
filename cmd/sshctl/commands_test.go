package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

func TestHelpAndUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "connect <alias>") {
		t.Fatalf("help missing connect usage: %s", out.String())
	}
	if err := run([]string{"not-a-command"}, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("expected unknown command error")
	}
}
func TestMetadataPersistsDescriptionAndGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.New()
	var out bytes.Buffer
	ssh := &openssh.Client{SSHConfig: filepath.Join(t.TempDir(), "sshconfig")}
	a := &app{in: strings.NewReader("development machine\nengineering\n"), out: &out, cfgPath: path, cfg: c, ssh: ssh}
	if err := a.metadata([]string{"buildbox"}, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Connections["buildbox"]
	if got.Description != "development machine" || got.Group != "engineering" {
		t.Fatalf("unexpected saved metadata: %#v", got)
	}
}
