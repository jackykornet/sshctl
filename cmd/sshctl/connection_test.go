package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// connTree is an app whose ssh_config Includes sshctl's file and defines
// web itself, with Groups prod and staging in the default Space, Space work
// (number 2) with Group api, and Managed connection web01 in prod.
func connTree(t *testing.T, in string) *app {
	t.Helper()
	a, _ := cliTree(t, openssh.IncludeLine+"\nHost web\n  HostName 10.0.0.5\n", config.New(), "")
	for _, args := range [][]string{
		{"group", "add", "prod"}, {"group", "add", "staging"},
		{"space", "add", "work"}, {"group", "--space", "work", "add", "api"},
		{"add", "web01", "--host", "10.0.0.1", "--user", "deploy", "--group", "prod", "--description", "site"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("sshctl %s: %v", strings.Join(args, " "), err)
		}
	}
	a.in = strings.NewReader(in)
	a.out.(interface{ Reset() }).Reset()
	return a
}

// managedHost returns alias's Host block in sshctl's file, and whether
// there is one.
func managedHost(t *testing.T, a *app, alias string) (openssh.Endpoint, bool) {
	t.Helper()
	hosts, err := a.ssh.ManagedHosts()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hosts {
		if h.Alias == alias {
			return h.Endpoint, true
		}
	}
	return openssh.Endpoint{}, false
}

func output(a *app) string { return a.out.(interface{ String() string }).String() }

func TestAddTakesEveryFieldAsAFlag(t *testing.T) {
	a := connTree(t, "")
	err := a.execute([]string{"add", "api01", "--host", "203.0.113.10", "--user", "ops", "--port", "2222",
		"--identity", "/keys/id_ed25519", "--space", "work", "--group", "api", "--description", "public site"})
	if err != nil {
		t.Fatal(err)
	}
	want := openssh.Endpoint{HostName: "203.0.113.10", User: "ops", Port: "2222", IdentityFile: "/keys/id_ed25519"}
	if got, ok := managedHost(t, a, "api01"); !ok || got != want {
		t.Errorf("api01's Host block = %+v (%v), want %+v", got, ok, want)
	}
	if got, want := loadCLI(t, a).Connections["api01"], (config.Connection{Alias: "api01", Description: "public site", Space: "work", Group: "api"}); !reflect.DeepEqual(got, want) {
		t.Errorf("api01 = %+v, want %+v", got, want)
	}
	if !strings.Contains(output(a), "added api01") {
		t.Errorf("output = %q, want it to say api01 was added", output(a))
	}
}

func TestEditWithAFlagChangesOnlyThatField(t *testing.T) {
	a := connTree(t, "")
	if err := a.execute([]string{"edit", "web01", "--port", "2200"}); err != nil {
		t.Fatal(err)
	}
	want := openssh.Endpoint{HostName: "10.0.0.1", User: "deploy", Port: "2200"}
	if got, _ := managedHost(t, a, "web01"); got != want {
		t.Errorf("web01's Host block = %+v, want %+v", got, want)
	}
	if got := loadCLI(t, a).Connections["web01"]; got.Group != "prod" || got.Description != "site" {
		t.Errorf("web01 = %+v, want its metadata kept", got)
	}
}

func TestEditClearsAFieldGivenEmpty(t *testing.T) {
	a := connTree(t, "")
	if err := a.execute([]string{"edit", "web01", "--user", ""}); err != nil {
		t.Fatal(err)
	}
	if got, _ := managedHost(t, a, "web01"); got.User != "" {
		t.Errorf("web01's User = %q, want none", got.User)
	}
}

func TestEditRenamesWithAlias(t *testing.T) {
	a := connTree(t, "")
	if err := a.execute([]string{"edit", "web01", "--alias", "web02"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := managedHost(t, a, "web01"); ok {
		t.Error("web01's Host block is still there")
	}
	if got, ok := managedHost(t, a, "web02"); !ok || got.HostName != "10.0.0.1" {
		t.Errorf("web02's Host block = %+v (%v)", got, ok)
	}
	if got := loadCLI(t, a).Connections["web02"]; got.Group != "prod" {
		t.Errorf("web02 = %+v, want web01's metadata", got)
	}
}

func TestGroupAndSpaceFlagsTakeNamesAndNumbers(t *testing.T) {
	a := connTree(t, "")
	for _, tc := range []struct {
		args        []string
		space, want string
	}{
		{[]string{"--group", "2"}, "", "staging"},
		{[]string{"--group", "0"}, "", ""},
		{[]string{"--space", "2", "--group", "1"}, "work", "api"},
		// Changing Space without a Group leaves the Connection ungrouped,
		// as the Workspace form does.
		{[]string{"--space", "1"}, "", ""},
	} {
		if err := a.execute(append([]string{"edit", "web01"}, tc.args...)); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got := loadCLI(t, a).Connections["web01"]; got.Space != tc.space || got.Group != tc.want {
			t.Errorf("%v: web01 in Space %q Group %q, want %q %q", tc.args, got.Space, got.Group, tc.space, tc.want)
		}
	}
}

func TestAddPromptsForEveryFieldWithoutFlags(t *testing.T) {
	a := connTree(t, "203.0.113.10\nops\n2222\n\nnew box\nwork\napi\n")
	if err := a.execute([]string{"add", "api01"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"HostName []: ", "User []: ", "Port []: ", "Identity file []: ", "Description []: ", "Space [default]: ", "Group []: "} {
		if !strings.Contains(output(a), p) {
			t.Errorf("prompts = %q, lack %q", output(a), p)
		}
	}
	want := openssh.Endpoint{HostName: "203.0.113.10", User: "ops", Port: "2222"}
	if got, _ := managedHost(t, a, "api01"); got != want {
		t.Errorf("api01's Host block = %+v, want %+v", got, want)
	}
	if got := loadCLI(t, a).Connections["api01"]; got.Space != "work" || got.Group != "api" || got.Description != "new box" {
		t.Errorf("api01 = %+v", got)
	}
}

func TestEditPromptsWithCurrentValuesAndEmptyKeepsThem(t *testing.T) {
	a := connTree(t, "\n\n\n2200\n\n\n\n\n")
	if err := a.execute([]string{"edit", "web01"}); err != nil {
		t.Fatal(err)
	}
	if want := "Alias [web01]: HostName [10.0.0.1]: User [deploy]: Port []: Identity file []: Description [site]: Space [default]: Group [prod]: "; !strings.Contains(output(a), want) {
		t.Errorf("prompts = %q, want %q", output(a), want)
	}
	want := openssh.Endpoint{HostName: "10.0.0.1", User: "deploy", Port: "2200"}
	if got, _ := managedHost(t, a, "web01"); got != want {
		t.Errorf("web01's Host block = %+v, want %+v", got, want)
	}
	if got := loadCLI(t, a).Connections["web01"]; got.Group != "prod" || got.Description != "site" {
		t.Errorf("web01 = %+v, want its metadata kept", got)
	}
}

func TestAddAndEditRefuseAnUnknownGroupWithAHint(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		in   string
	}{
		{"add flag", []string{"add", "new", "--host", "h", "--group", "nowhere"}, ""},
		{"edit flag", []string{"edit", "web01", "--group", "nowhere"}, ""},
		{"edit prompt", []string{"edit", "web"}, "\n\nnowhere\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := connTree(t, tc.in)
			before := loadCLI(t, a).Connections
			err := a.execute(tc.args)
			if err == nil || !strings.Contains(err.Error(), "sshctl group add nowhere") {
				t.Fatalf("err = %v, want a hint to run sshctl group add nowhere", err)
			}
			if _, ok := managedHost(t, a, "new"); ok {
				t.Error("a Host block was written for new")
			}
			if got := loadCLI(t, a).Connections; len(got) != len(before) || !reflect.DeepEqual(got["web01"], before["web01"]) {
				t.Errorf("metadata = %+v, want it unchanged from %+v", got, before)
			}
		})
	}
}

func TestAddAndEditRefuseAnInvalidValueWithoutWriting(t *testing.T) {
	for _, args := range [][]string{
		{"add", "new", "--host", "h", "--port", "abc", "--description", "x"},
		{"add", "new", "--description", "no host"},
		{"edit", "web01", "--port", "0", "--description", "changed"},
		{"edit", "web01", "--host", "a b"},
		{"add", "new", "--host", "h", "--space", "nowhere"},
	} {
		a := connTree(t, "")
		managed, _ := os.ReadFile(a.ssh.ManagedPath())
		if err := a.execute(args); err == nil {
			t.Errorf("sshctl %s succeeded", strings.Join(args, " "))
		}
		if after, _ := os.ReadFile(a.ssh.ManagedPath()); string(after) != string(managed) {
			t.Errorf("sshctl %s rewrote the Host blocks:\n%s", strings.Join(args, " "), after)
		}
		got := loadCLI(t, a).Connections
		if _, ok := got["new"]; ok || got["web01"].Description != "site" {
			t.Errorf("sshctl %s changed metadata: %+v", strings.Join(args, " "), got)
		}
	}
}

// web's Host block is the user's: edit changes only its sshctl metadata,
// says so, and refuses Host fields.
func TestEditOfAConnectionDefinedOutsideSshctlChangesOnlyMetadata(t *testing.T) {
	a := connTree(t, "front end\n\nstaging\n")
	if err := a.execute([]string{"edit", "web"}); err != nil {
		t.Fatal(err)
	}
	out := output(a)
	if !strings.Contains(out, "defined outside sshctl") {
		t.Errorf("output = %q, want it to say web is defined outside sshctl", out)
	}
	if strings.Contains(out, "HostName") || !strings.Contains(out, "Description []: Space [default]: Group []: ") {
		t.Errorf("prompts = %q, want only description, Space and group", out)
	}
	if got := loadCLI(t, a).Connections["web"]; got.Description != "front end" || got.Group != "staging" {
		t.Errorf("web = %+v", got)
	}
	if _, ok := managedHost(t, a, "web"); ok {
		t.Error("edit wrote a Host block for web")
	}
	for _, flag := range []string{"--host", "--user", "--port", "--identity", "--alias"} {
		err := a.execute([]string{"edit", "web", flag, "x", "--description", "changed"})
		if err == nil || !strings.Contains(err.Error(), "defined outside sshctl") {
			t.Errorf("edit web %s: err = %v, want a refusal", flag, err)
		}
	}
	if got := loadCLI(t, a).Connections["web"]; got.Description != "front end" {
		t.Errorf("web = %+v, want the refused edits to change nothing", got)
	}
}

func TestAddRefusesAnExistingConnectionAndEditAnUnknownOne(t *testing.T) {
	a := connTree(t, "")
	for _, alias := range []string{"web", "web01"} {
		if err := a.execute([]string{"add", alias, "--host", "h"}); err == nil || !strings.Contains(err.Error(), "sshctl edit "+alias) {
			t.Errorf("add %s: err = %v, want a hint to use sshctl edit", alias, err)
		}
	}
	if err := a.execute([]string{"edit", "nope", "--port", "22"}); err == nil || !strings.Contains(err.Error(), "sshctl add nope") {
		t.Errorf("edit nope: err = %v, want a hint to use sshctl add", err)
	}
}

func TestAddOffersTheInclude(t *testing.T) {
	for _, tc := range []struct {
		answer string
		saved  bool
	}{{"y\n", true}, {"n\n", false}} {
		a, out := cliTree(t, "Host web\n", config.New(), tc.answer)
		err := a.execute([]string{"add", "new", "--host", "h"})
		if got := err == nil; got != tc.saved {
			t.Fatalf("answer %q: err = %v", tc.answer, err)
		}
		if !strings.Contains(out.String(), openssh.IncludeLine) {
			t.Errorf("answer %q: output %q does not ask about the Include", tc.answer, out)
		}
		if _, ok := managedHost(t, a, "new"); ok != tc.saved {
			t.Errorf("answer %q: Host block written %v, want %v", tc.answer, ok, tc.saved)
		}
	}
}

func TestAddAndEditRefuseBadFlags(t *testing.T) {
	a := connTree(t, "")
	for _, args := range [][]string{
		{"add"}, {"add", "a", "b"}, {"add", "new", "--bogus", "x"}, {"add", "new", "--host"},
		{"add", "new", "--alias", "x"}, {"edit", "web01", "--port", "1", "--port", "2"},
	} {
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), "usage: sshctl "+args[0]) {
			t.Errorf("sshctl %s: err = %v, want the usage", strings.Join(args, " "), err)
		}
	}
}

// remove does what the Workspace's d does: it deletes the Host block when
// sshctl manages it, and the sshctl metadata, after asking.
func TestRemoveDeletesWhatTheWorkspaceDeletes(t *testing.T) {
	a := connTree(t, "y\n")
	if err := a.execute([]string{"remove", "web01"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := managedHost(t, a, "web01"); ok {
		t.Error("web01's Host block is still there")
	}
	if _, ok := loadCLI(t, a).Connections["web01"]; ok {
		t.Error("web01's metadata is still there")
	}

	a = connTree(t, "n\n")
	if err := a.execute([]string{"remove", "web01"}); err == nil {
		t.Error("remove went ahead after no")
	}
	if _, ok := managedHost(t, a, "web01"); !ok {
		t.Error("remove deleted web01's Host block after no")
	}
	if err := a.execute([]string{"remove", "web01", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := managedHost(t, a, "web01"); ok {
		t.Error("remove --yes kept web01's Host block")
	}
}

func TestRemoveLeavesAUserHostBlock(t *testing.T) {
	a := connTree(t, "")
	if err := a.execute([]string{"remove", "web"}); err == nil || !strings.Contains(err.Error(), "defined outside sshctl") {
		t.Errorf("remove web without metadata: err = %v", err)
	}
	if err := a.execute([]string{"edit", "web", "--description", "front"}); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"remove", "web", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadCLI(t, a).Connections["web"]; ok {
		t.Error("web's metadata is still there")
	}
	if data, _ := os.ReadFile(a.ssh.SSHConfig); !strings.Contains(string(data), "Host web\n") {
		t.Errorf("ssh_config lost web's Host block:\n%s", data)
	}
	if err := a.execute([]string{"remove", "nope", "--yes"}); err == nil {
		t.Error("remove nope succeeded")
	}
}

// Tunnels have no add or edit field: edit keeps them, and remove deletes
// them, also from a Connection whose only metadata is Tunnels.
func TestEditKeepsTunnelsAndRemoveDeletesThem(t *testing.T) {
	a := connTree(t, "")
	for _, alias := range []string{"web01", "web"} {
		if err := a.execute([]string{"tunnel", "add", alias, "panel=8083"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.execute([]string{"edit", "web01", "--port", "2200"}); err != nil {
		t.Fatal(err)
	}
	if got := loadCLI(t, a).Connections["web01"].Tunnels; len(got) != 1 || got[0].Name != "panel" {
		t.Errorf("web01's Tunnels after edit = %+v, want panel", got)
	}
	for _, alias := range []string{"web01", "web"} {
		if err := a.execute([]string{"remove", alias, "--yes"}); err != nil {
			t.Fatal(err)
		}
		if m, ok := loadCLI(t, a).Connections[alias]; ok {
			t.Errorf("%s's metadata after remove = %+v, want none", alias, m)
		}
	}
}
