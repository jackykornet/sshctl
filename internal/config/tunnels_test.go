package config

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/tunnel"
)

var panel = tunnel.Tunnel{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}

func TestLoadVersion5HasNoTunnelsAndSavesVersion6(t *testing.T) {
	path := writeConfig(t, `{"version":5,"spaces":[{"name":"home","default":true,"number":1}],
		"connections":{"web":{"alias":"web","description":"front end"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := c.Connections["web"]; m.Description != "front end" || m.Tunnels != nil {
		t.Errorf("web = %+v, want its description and no Tunnels", m)
	}
	if again, err := Load(path, nil); err != nil || again.Version != Version {
		t.Errorf("saved version %v (%v), want %d", again.Version, err, Version)
	}
}

func TestTunnelsSurviveASaveRoundTrip(t *testing.T) {
	path := writeConfig(t, `{"version":6,"connections":{}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := tunnel.Tunnel{LocalPort: 9000, RemoteHost: "10.0.0.5", RemotePort: 80}
	c.SetTunnels("web", []tunnel.Tunnel{panel, other})
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"tunnels"`, `"local_port": 8083`, `"remote_host": "10.0.0.5"`, `"remote_port": 80`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("config.json lacks %s:\n%s", key, b)
		}
	}
	again, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Connections["web"].Tunnels; !slices.Equal(got, []tunnel.Tunnel{panel, other}) {
		t.Errorf("Tunnels = %+v", got)
	}
}

// Tunnels are metadata: a Connection with only Tunnels keeps its record, and
// changing its description, group or Space leaves them alone.
func TestTunnelsCountAsMetadata(t *testing.T) {
	c := New()
	c.SetTunnels("web", []tunnel.Tunnel{panel})
	c.SetMetadata("web", "", "front end", "")
	c.SetMetadata("web", "", "", "")
	m, ok := c.Connections["web"]
	if !ok || !slices.Equal(m.Tunnels, []tunnel.Tunnel{panel}) || m.Description != "" {
		t.Fatalf("web = %+v, %v; want only its Tunnel left", m, ok)
	}
	c.SetTunnels("web", nil)
	if m, ok := c.Connections["web"]; ok {
		t.Errorf("web = %+v, want no metadata left", m)
	}
}
