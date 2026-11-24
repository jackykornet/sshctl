package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

// decodeJSON decodes what a command printed, so a test compares the field
// names and values scripts see.
func decodeJSON(t *testing.T, out string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	return v
}

func TestListJSONShowsEveryConnectionWithSpaceAndGroupNames(t *testing.T) {
	cfg := config.New()
	cfg.Spaces = []config.Space{{Name: "home", Number: 1, Default: true}, {Name: "acme", Number: 2}}
	cfg.Groups = []config.Group{{Name: "prod"}, {Name: "prod", Space: "acme"}}
	cfg.Connections["web"] = config.Connection{Alias: "web", Description: "front end", Group: "prod"}
	cfg.Connections["db"] = config.Connection{Alias: "db", Space: "acme", Group: "prod"}
	cfg.Connections["gone"] = config.Connection{Alias: "gone", Description: "retired"}
	a, out := cliTree(t, "Host web\nHost db\nHost list\n", cfg, "")
	if err := a.execute([]string{"list", "--json"}); err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"alias": "web", "description": "front end", "space": "home", "group": "prod", "orphaned": false, "shadowedByCommand": false},
		map[string]any{"alias": "db", "description": "", "space": "acme", "group": "prod", "orphaned": false, "shadowedByCommand": false},
		map[string]any{"alias": "list", "description": "", "space": "home", "group": "", "orphaned": false, "shadowedByCommand": true},
		map[string]any{"alias": "gone", "description": "retired", "space": "home", "group": "", "orphaned": true, "shadowedByCommand": false},
	}
	if got := decodeJSON(t, out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("list --json:\n%s\nwant:\n%v", out.String(), want)
	}
}

func TestGroupListJSONShowsEveryGroupWithItsSpace(t *testing.T) {
	a, out := spaceTree(t, "")
	cfg := loadCLI(t, a)
	for i, g := range cfg.Groups {
		if g.Name == "data" {
			cfg.Groups[i].Dotfiles = config.DotfilesNone
		}
		if g.Name == "prod" && g.Space == "" {
			cfg.Groups[i].Dotfiles = "https://example.com/d.git"
		}
	}
	if err := config.Save(a.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.execute([]string{"group", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"space": "home", "number": 1.0, "name": "prod", "connections": 1.0, "dotfiles": "https://example.com/d.git"},
		map[string]any{"space": "acme", "number": 1.0, "name": "prod", "connections": 1.0, "dotfiles": ""},
		map[string]any{"space": "acme", "number": 2.0, "name": "data", "connections": 0.0, "dotfiles": "none"},
	}
	if got := decodeJSON(t, out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("group list --json:\n%s\nwant:\n%v", out.String(), want)
	}

	out.Reset()
	if err := a.execute([]string{"group", "--space", "empty", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Errorf("group list --json --space empty = %q, want []", out.String())
	}
}

func TestSpaceListJSONShowsEverySpace(t *testing.T) {
	a, out := spaceTree(t, "")
	key := keyFile(t, "id_acme")
	for _, args := range [][]string{
		{"space", "dotfiles", "acme", "https://example.com/acme.git"},
		{"key", "use", "--space", "acme", key},
		{"space", "list", "--json"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := []any{
		map[string]any{"number": 1.0, "name": "home", "default": true, "groups": 1.0, "connections": 3.0, "dotfiles": "", "identity": ""},
		map[string]any{"number": 2.0, "name": "acme", "default": false, "groups": 2.0, "connections": 1.0, "dotfiles": "https://example.com/acme.git", "identity": key},
		map[string]any{"number": 4.0, "name": "empty", "default": false, "groups": 0.0, "connections": 0.0, "dotfiles": "", "identity": ""},
	}
	if got := decodeJSON(t, out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("space list --json:\n%s\nwant:\n%v", out.String(), want)
	}
}

// A Space without a number (beyond the ninth in an older config) has number
// null.
func TestSpaceListJSONShowsNoNumberAsNull(t *testing.T) {
	cfg := config.New()
	cfg.Spaces = []config.Space{{Name: "extra"}}
	for n := 1; n <= config.MaxSpaces; n++ {
		cfg.Spaces = append(cfg.Spaces, config.Space{Name: "s" + strconv.Itoa(n), Number: n, Default: n == 1})
	}
	a, out := cliTree(t, "", cfg, "")
	if err := a.execute([]string{"space", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	spaces := decodeJSON(t, out.String()).([]any)
	last := spaces[len(spaces)-1].(map[string]any)
	if n, ok := last["number"]; last["name"] != "extra" || !ok || n != nil {
		t.Errorf("last Space = %v, want extra with number null", last)
	}
}

func TestListJSONWithSpaceAndNothingInItIsAnEmptyArray(t *testing.T) {
	a, out := spaceTree(t, "")
	if err := a.execute([]string{"list", "--json", "--space", "empty"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Errorf("list --json --space empty = %q, want []", out.String())
	}
}

// info prints one Connection: its Space and Group by name, whether sshctl
// writes its Host block, whether its alias is gone, and the Selected identity
// it would use, its Space's own or else the global one.
func TestInfoShowsAConnectionAndTheIdentityItUses(t *testing.T) {
	a := connTree(t, "")
	out := a.out.(*bytes.Buffer)
	global, work := keyFile(t, "id_global"), keyFile(t, "id_work")
	for _, args := range [][]string{
		{"key", "use", global},
		{"key", "use", "--space", "work", work},
		{"add", "api01", "--host", "10.0.0.2", "--space", "work", "--group", "api"},
		{"edit", "web", "--description", "own block"},
	} {
		if err := a.execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	cfg := loadCLI(t, a)
	cfg.Connections["gone"] = config.Connection{Alias: "gone", Group: "staging"}
	if err := config.Save(a.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want map[string]any
	}{
		{[]string{"info", "web01"}, map[string]any{"alias": "web01", "space": "default", "group": "prod", "description": "site", "managed": true, "orphaned": false, "identity": global}},
		{[]string{"info", "api01"}, map[string]any{"alias": "api01", "space": "work", "group": "api", "description": "", "managed": true, "orphaned": false, "identity": work}},
		{[]string{"info", "web", "--json"}, map[string]any{"alias": "web", "space": "default", "group": "", "description": "own block", "managed": false, "orphaned": false, "identity": global}},
		{[]string{"info", "gone"}, map[string]any{"alias": "gone", "space": "default", "group": "staging", "description": "", "managed": false, "orphaned": true, "identity": global}},
	} {
		out.Reset()
		if err := a.execute(tc.args); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got := decodeJSON(t, out.String()); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("sshctl %v:\n%s\nwant:\n%v", tc.args, out.String(), tc.want)
		}
	}
}

func TestInfoRefusesAnUnknownAlias(t *testing.T) {
	a := connTree(t, "")
	out := a.out.(*bytes.Buffer)
	err := a.execute([]string{"info", "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("info nope: err = %v, want one naming nope", err)
	}
	if out.Len() != 0 {
		t.Errorf("info nope printed %q, want nothing on stdout", out)
	}
	for _, args := range [][]string{{"info"}, {"info", "web", "web01"}} {
		if err := a.execute(args); err == nil || !strings.Contains(err.Error(), "usage: sshctl info") {
			t.Errorf("sshctl %v: err = %v, want the usage", args, err)
		}
	}
}
