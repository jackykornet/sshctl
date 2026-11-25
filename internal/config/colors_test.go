package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestLoadVersion6HasNoColorsAndSavesVersion7(t *testing.T) {
	path := writeConfig(t, `{"version":6,"spaces":[{"name":"home","default":true,"number":1}],
		"groups":[{"name":"prod"}],"connections":{"web":{"alias":"web","group":"prod"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Groups[0].Color != "" || c.Spaces[0].Color != "" {
		t.Errorf("colors = %q, %q, want none", c.Groups[0].Color, c.Spaces[0].Color)
	}
	if again, err := Load(path, nil); err != nil || again.Version != 7 {
		t.Errorf("saved version %v (%v), want 7", again.Version, err)
	}
}

func TestColorsAreSavedOnGroupAndSpaceRecordsInConfigJSON(t *testing.T) {
	path := writeConfig(t, `{"version":7,"spaces":[{"name":"default","default":true,"number":1},{"name":"work","number":2}],
		"groups":[{"name":"prod","space":"work"}],"connections":{}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Groups[0].Color = "red"
	c.Spaces[1].Color = "cyan"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), `"color"`) != 2 {
		t.Errorf("config.json should hold both colors:\n%s", b)
	}
	if s, err := os.ReadFile(StatePath(path)); err != nil || strings.Contains(string(s), "color") {
		t.Errorf("state.json holds a color (%v):\n%s", err, s)
	}
	again, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Groups[0].Color != "red" || again.Spaces[1].Color != "cyan" {
		t.Errorf("reloaded colors = %q, %q", again.Groups[0].Color, again.Spaces[1].Color)
	}
}

func TestLoadReportsAnUnknownColorAndKeepsIt(t *testing.T) {
	path := writeConfig(t, `{"version":7,"spaces":[{"name":"default","default":true,"number":1,"color":"pink"}],
		"groups":[{"name":"prod","color":"#ff0000"}],"connections":{}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	notices := strings.Join(c.Notices, "\n")
	for _, want := range []string{`"pink"`, `"#ff0000"`, "red, yellow, green, cyan, blue, magenta"} {
		if !strings.Contains(notices, want) {
			t.Errorf("notices %q lack %s", notices, want)
		}
	}
	if c.Groups[0].Color != "#ff0000" {
		t.Errorf("group color = %q, want it kept", c.Groups[0].Color)
	}
}

func TestParseColorTakesNoneForNoColor(t *testing.T) {
	for in, want := range map[string]string{"none": "", "red": "red", "magenta": "magenta"} {
		if got, err := ParseColor(in); err != nil || got != want {
			t.Errorf("ParseColor(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "pink", "RED"} {
		if _, err := ParseColor(in); !errors.Is(err, ErrUnknownColor) {
			t.Errorf("ParseColor(%q) = %v, want ErrUnknownColor", in, err)
		}
	}
}

func TestCheckColor(t *testing.T) {
	for _, name := range append([]string{""}, Colors...) {
		if err := CheckColor(name); err != nil {
			t.Errorf("CheckColor(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"pink", "Red", "#ff0000", "none"} {
		err := CheckColor(name)
		if !errors.Is(err, ErrUnknownColor) || !strings.Contains(err.Error(), "none, red, yellow, green, cyan, blue, magenta") {
			t.Errorf("CheckColor(%q) = %v, want ErrUnknownColor listing the colors", name, err)
		}
	}
}
