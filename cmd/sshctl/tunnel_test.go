package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/tunnel"
)

func TestTunnelAddListAndRemove(t *testing.T) {
	a, out := cliTree(t, "Host web\nHost db\n", config.New(), "")
	for _, args := range [][]string{
		{"tunnel", "add", "web", "panel=8083"},
		{"tunnel", "add", "web", "9000:10.0.0.5:80"},
		{"tunnel", "add", "db", "5433:localhost:5432"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if out.Len() != 0 {
		t.Errorf("add printed %q, want nothing", out.String())
	}
	if err := a.execute([]string{"tunnel", "list", "web"}); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"panel  http://localhost:8083 → localhost:8083\n" +
		"9000   http://localhost:9000 → 10.0.0.5:80\n"
	if out.String() != want {
		t.Errorf("tunnel list web:\n%s\nwant:\n%s", out, want)
	}
	out.Reset()
	if err := a.execute([]string{"tunnel", "list"}); err != nil {
		t.Fatal(err)
	}
	want = "" +
		"web  panel  http://localhost:8083 → localhost:8083\n" +
		"web  9000   http://localhost:9000 → 10.0.0.5:80\n" +
		"db   5433   http://localhost:5433 → localhost:5432\n"
	if out.String() != want {
		t.Errorf("tunnel list:\n%s\nwant:\n%s", out, want)
	}
	for _, ref := range []string{"panel", "9000"} {
		if err := a.execute([]string{"tunnel", "rm", "web", ref}); err != nil {
			t.Fatal(err)
		}
	}
	if got := load(t, a).Connections; len(got["web"].Tunnels) != 0 || len(got["db"].Tunnels) != 1 {
		t.Errorf("Connections = %+v, want only db's Tunnel left", got)
	}
}

func TestTunnelAddAndRemoveRefuse(t *testing.T) {
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Tunnels: []tunnel.Tunnel{{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}}}
	a, _ := cliTree(t, "Host web\n", cfg, "")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"tunnel", "add", "web", "8083"}, "local port 8083"},
		{[]string{"tunnel", "add", "web", "panel=9000"}, `named "panel"`},
		{[]string{"tunnel", "add", "web", "70000"}, "1 to 65535"},
		{[]string{"tunnel", "add", "web", "42=9000"}, "only digits"},
		{[]string{"tunnel", "add", "nope", "9000"}, `no such connection "nope"`},
		{[]string{"tunnel", "rm", "web", "db"}, `no such tunnel "db"`},
		{[]string{"tunnel", "add", "web"}, "usage: sshctl tunnel"},
		{[]string{"tunnel", "rm", "web"}, "usage: sshctl tunnel"},
		{[]string{"tunnel", "list", "web", "db"}, "usage: sshctl tunnel"},
		{[]string{"tunnel"}, "usage: sshctl tunnel"},
	} {
		if err := a.execute(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want it to mention %q", tc.args, err, tc.want)
		}
	}
}

func TestTunnelRunsTheConnectionsTunnelsInTheForeground(t *testing.T) {
	tunnels := []tunnel.Tunnel{
		{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083},
		{LocalPort: 9000, RemoteHost: "10.0.0.5", RemotePort: 80},
	}
	cfg := config.New()
	cfg.SelectedIdentity = "/k/id"
	cfg.Connections["web"] = config.Connection{Alias: "web", Tunnels: tunnels}
	a, out := cliTree(t, "Host web\n", cfg, "")
	r := &sshRecorder{}
	a.runSSH = r.run
	if err := a.execute([]string{"tunnel", "web"}); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{openssh.TunnelArgs("web", "/k/id", tunnels, false)}; !reflect.DeepEqual(r.calls, want) {
		t.Errorf("ran %v, want %v", r.calls, want)
	}
	if strings.Contains(strings.Join(r.calls[0], " "), "BatchMode") {
		t.Error("a foreground tunnel may prompt, so it must not use BatchMode")
	}
	want := "" +
		"panel  http://localhost:8083 → localhost:8083\n" +
		"9000   http://localhost:9000 → 10.0.0.5:80\n"
	if !strings.HasPrefix(out.String(), want) {
		t.Errorf("printed:\n%s\nwant it to start with:\n%s", out, want)
	}
	if h := load(t, a).History; len(h) != 0 {
		t.Errorf("History = %v, want a tunnel not to record one", h)
	}
}

func TestTunnelReturnsSSHsExitStatus(t *testing.T) {
	exit := exitCodeError(255)
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Tunnels: []tunnel.Tunnel{{LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}}}
	a, _ := cliTree(t, "Host web\n", cfg, "")
	a.runSSH = (&sshRecorder{fail: exit}).run
	err := a.execute([]string{"tunnel", "web"})
	if !errors.Is(err, exit) || exitCode(err) != 255 {
		t.Errorf("err = %v (exit code %d), want ssh's exit status 255", err, exitCode(err))
	}
}

func TestTunnelWithNoTunnelsSaysHowToAddOne(t *testing.T) {
	a, _ := cliTree(t, "Host web\n", config.New(), "")
	r := &sshRecorder{}
	a.runSSH = r.run
	err := a.execute([]string{"tunnel", "web"})
	if err == nil || !strings.Contains(err.Error(), "sshctl tunnel add web "+tunnel.SpecSyntax) {
		t.Errorf("err = %v, want how to add a tunnel", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("ran %v, want no ssh", r.calls)
	}
}

func TestTunnelRefusesAnUnknownConnection(t *testing.T) {
	a, _ := cliTree(t, "Host web\n", config.New(), "")
	for _, args := range [][]string{{"tunnel", "nope"}, {"tunnel", "list", "nope"}} {
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), `no such connection "nope"`) {
			t.Errorf("%v: err = %v, want no such connection", args, err)
		}
	}
}

// add and edit save through catalog.Save, which replaces Tunnels: they keep
// the Connection's own.
func TestEditKeepsTunnels(t *testing.T) {
	tunnels := []tunnel.Tunnel{{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}}
	cfg := config.New()
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "front end", Tunnels: tunnels}
	a, _ := cliTree(t, "Host web\n", cfg, "site\n\n")
	if err := a.metadata([]string{"web"}, true); err != nil {
		t.Fatal(err)
	}
	if got := load(t, a).Connections["web"]; got.Description != "site" || !reflect.DeepEqual(got.Tunnels, tunnels) {
		t.Errorf("web = %+v, want the new description and its Tunnels", got)
	}
}
