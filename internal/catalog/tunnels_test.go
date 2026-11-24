package catalog

import (
	"errors"
	"slices"
	"testing"
)

var (
	panel = Tunnel{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}
	db    = Tunnel{LocalPort: 5433, RemoteHost: "10.0.0.5", RemotePort: 5432}
)

func (tr *tree) tunnels(t *testing.T, alias string) []Tunnel {
	t.Helper()
	got, err := tr.cat.Tunnels(alias)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAddTunnelListsThemInOrderAndInTheSnapshot(t *testing.T) {
	tr := newTree(t, false, "Host web\n  HostName 10.0.0.1\n")
	for _, tun := range []Tunnel{panel, db} {
		if err := tr.cat.AddTunnel("web", tun); err != nil {
			t.Fatal(err)
		}
	}
	if got := tr.tunnels(t, "web"); !slices.Equal(got, []Tunnel{panel, db}) {
		t.Errorf("Tunnels = %+v", got)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := snap.Entry("web"); !slices.Equal(e.Tunnels, []Tunnel{panel, db}) {
		t.Errorf("snapshot web = %+v, want its Tunnels", e)
	}
}

func TestAddTunnelRefusesDuplicatesAndUnknownConnections(t *testing.T) {
	tr := newTree(t, false, "Host web\n")
	if err := tr.cat.AddTunnel("web", panel); err != nil {
		t.Fatal(err)
	}
	samePort := db
	samePort.LocalPort = panel.LocalPort
	if err := tr.cat.AddTunnel("web", samePort); !errors.Is(err, ErrDuplicateTunnel) {
		t.Errorf("same local port: err = %v, want ErrDuplicateTunnel", err)
	}
	if err := tr.cat.AddTunnel("nope", db); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("unknown alias: err = %v, want ErrUnknownConnection", err)
	}
	if got := tr.tunnels(t, "web"); !slices.Equal(got, []Tunnel{panel}) {
		t.Errorf("Tunnels = %+v, want only panel", got)
	}
}

func TestRemoveTunnelByNameOrPortAndDropsEmptyMetadata(t *testing.T) {
	tr := newTree(t, false, "Host web\n")
	for _, tun := range []Tunnel{panel, db} {
		if err := tr.cat.AddTunnel("web", tun); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := tr.cat.RemoveTunnel("web", "panel"); err != nil || got != panel {
		t.Fatalf("RemoveTunnel(panel) = %+v, %v", got, err)
	}
	if _, err := tr.cat.RemoveTunnel("web", "panel"); !errors.Is(err, ErrNoTunnel) {
		t.Errorf("removing again: err = %v, want ErrNoTunnel", err)
	}
	if got, err := tr.cat.RemoveTunnel("web", "5433"); err != nil || got != db {
		t.Fatalf("RemoveTunnel(5433) = %+v, %v", got, err)
	}
	if _, ok := tr.config(t).Connections["web"]; ok {
		t.Error("web has no metadata left and should have no record")
	}
}

func TestRenameCarriesTunnelsAndAnOrphanKeepsThem(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.Save("", Spec{Alias: "api", Managed: true, Host: Host{HostName: "h"}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddTunnel("api", panel); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.Save("api", Spec{Alias: "api2", Description: "API", Managed: true, Host: Host{HostName: "h"}}); err != nil {
		t.Fatal(err)
	}
	if got := tr.tunnels(t, "api2"); !slices.Equal(got, []Tunnel{panel}) {
		t.Errorf("api2 Tunnels = %+v, want panel carried over", got)
	}
	if got := tr.tunnels(t, "api"); len(got) != 0 {
		t.Errorf("api Tunnels = %+v, want none", got)
	}
	// Losing its Host block leaves an Orphaned connection with its Tunnels.
	if err := tr.ssh.DeleteHost("api2"); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := snap.Entry("api2"); !ok || !e.Orphaned || !slices.Equal(e.Tunnels, []Tunnel{panel}) {
		t.Errorf("api2 = %+v, %v; want an Orphaned connection with its Tunnel", e, ok)
	}
}
