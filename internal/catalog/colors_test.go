package catalog

import (
	"errors"
	"testing"

	"github.com/sshctl/sshctl/internal/config"
)

func TestColorResolvesGroupThenSpaceThenNone(t *testing.T) {
	for _, tc := range []struct {
		name         string
		space, group string
		grouped      bool
		want         string
	}{
		{"group wins", "cyan", "red", true, "red"},
		{"unset group falls back to the space", "cyan", "", true, "cyan"},
		{"ungrouped uses the space", "cyan", "red", false, "cyan"},
		{"nothing set is none", "", "", true, ""},
		{"an unknown group color counts as none", "cyan", "pink", true, "cyan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, true, "Host web\n")
			cfg := config.New()
			cfg.Spaces = append(cfg.Spaces, config.Space{Name: "acme", Number: 2, Color: tc.space})
			cfg.Groups = []config.Group{{Name: "prod", Space: "acme", Color: tc.group}}
			group := ""
			if tc.grouped {
				group = "prod"
			}
			cfg.Connections["web"] = config.Connection{Alias: "web", Space: "acme", Group: group}
			tr.seed(t, cfg)
			snap, err := tr.cat.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if got := snap.Color("acme", group); got != tc.want {
				t.Errorf("Color = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetGroupAndSpaceColorSaveAndClear(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.AddGroup("acme", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetGroupColor("acme", "prod", "red"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpaceColor("acme", "cyan"); err != nil {
		t.Fatal(err)
	}
	cfg := tr.config(t)
	if g, s := cfg.Groups[0].Color, cfg.Spaces[1].Color; g != "red" || s != "cyan" {
		t.Errorf("colors = %q, %q; want red, cyan", g, s)
	}
	if err := tr.cat.SetGroupColor("acme", "prod", ""); err != nil {
		t.Fatal(err)
	}
	if got := tr.config(t).Groups[0].Color; got != "" {
		t.Errorf("cleared group color = %q", got)
	}
	if err := tr.cat.SetGroupColor("acme", "prod", "pink"); !errors.Is(err, config.ErrUnknownColor) {
		t.Errorf("SetGroupColor(pink) = %v, want ErrUnknownColor", err)
	}
	if err := tr.cat.SetSpaceColor("acme", "#ff0000"); !errors.Is(err, config.ErrUnknownColor) {
		t.Errorf("SetSpaceColor(#ff0000) = %v, want ErrUnknownColor", err)
	}
	if err := tr.cat.SetGroupColor("acme", "nope", "red"); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("SetGroupColor(nope) = %v, want ErrUnknownGroup", err)
	}
	if err := tr.cat.SetSpaceColor("nope", "red"); !errors.Is(err, ErrUnknownSpace) {
		t.Errorf("SetSpaceColor(nope) = %v, want ErrUnknownSpace", err)
	}
}

func TestColorsSurviveRenamingAndMovingAGroupOrSpace(t *testing.T) {
	tr := newTree(t, true, "")
	if err := tr.cat.AddSpace("acme"); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"prod", "dev"} {
		if err := tr.cat.AddGroup("acme", g); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.cat.SetGroupColor("acme", "prod", "red"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpaceColor("acme", "yellow"); err != nil {
		t.Fatal(err)
	}
	if err := tr.cat.SetSpaceColor("", "green"); err != nil {
		t.Fatal(err)
	}
	steps := []func() error{
		func() error { return tr.cat.RenameGroup("acme", "prod", "live") },
		func() error { return tr.cat.MoveGroup("acme", "live", 2) },
		func() error { return tr.cat.RenameSpace("acme", "globex") },
		func() error { return tr.cat.MoveSpace("globex", 5) },
		func() error { return tr.cat.RenameSpace("default", "home") },
		func() error { return tr.cat.MoveSpace("home", 3) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Color("globex", "live"); got != "red" {
		t.Errorf("renamed, moved group color = %q, want red", got)
	}
	if got := snap.Color("globex", "dev"); got != "yellow" {
		t.Errorf("renamed, moved space color = %q, want yellow", got)
	}
	if got := snap.Color("", ""); got != "green" {
		t.Errorf("renamed, moved default space color = %q, want green", got)
	}
}
