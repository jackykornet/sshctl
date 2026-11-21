package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestLoadPutsAnOlderConfigInTheDefaultSpace(t *testing.T) {
	path := writeConfig(t, `{"version":3,"groups":[{"name":"prod"}],
		"connections":{"web":{"alias":"web","description":"site","group":"prod"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Space{{Name: DefaultSpaceName, Number: 1, Default: true}}; !reflect.DeepEqual(c.Spaces, want) {
		t.Errorf("Spaces = %+v, want %+v", c.Spaces, want)
	}
	if c.Version != Version || c.Groups[0].Space != "" || c.Connections["web"].Space != "" {
		t.Errorf("Load = %+v, want version %d with everything in the default Space", c, Version)
	}
	again, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Spaces, c.Spaces) {
		t.Errorf("reloaded Spaces = %+v, want them saved as %+v", again.Spaces, c.Spaces)
	}
}

func TestLoadGivesASpaceNamedOnlyByAGroupOrConnectionARecord(t *testing.T) {
	path := writeConfig(t, `{"version":4,"spaces":[{"name":"home","default":true,"number":1}],
		"groups":[{"name":"prod","space":"acme"}],
		"connections":{"pi":{"alias":"pi","space":"lab"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Space{{Name: "home", Number: 1, Default: true}, {Name: "acme", Number: 2}, {Name: "lab", Number: 3}}
	if !reflect.DeepEqual(c.Spaces, want) {
		t.Errorf("Spaces = %+v, want %+v", c.Spaces, want)
	}
}

func TestGroupsWithOneNameInTwoSpacesStayApart(t *testing.T) {
	path := writeConfig(t, `{"version":4,"spaces":[{"name":"default","default":true,"number":1},{"name":"acme","number":2}],
		"connections":{"web":{"alias":"web","group":"prod"},"api":{"alias":"api","group":"prod","space":"acme"}}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Group{{Name: "prod"}, {Name: "prod", Space: "acme"}}; !reflect.DeepEqual(c.Groups, want) {
		t.Fatalf("Groups = %+v, want %+v", c.Groups, want)
	}
	c.CollapsedGroups = []string{"prod", "acme/prod"}
	c.RenameGroup("acme", "prod", "live")
	if c.Connections["web"].Group != "prod" || c.Connections["api"].Group != "live" {
		t.Errorf("after renaming acme's prod: web in %q, api in %q", c.Connections["web"].Group, c.Connections["api"].Group)
	}
	if want := []string{"prod", "acme/live"}; !reflect.DeepEqual(c.CollapsedGroups, want) {
		t.Errorf("collapsed = %v, want %v", c.CollapsedGroups, want)
	}
}

func TestRenameSpaceCarriesItsMembersAndThisMachinesState(t *testing.T) {
	c := New()
	c.Spaces = append(c.Spaces, Space{Name: "acme"})
	c.Groups = []Group{{Name: "prod", Space: "acme"}, {Name: "prod"}}
	c.SetMetadata("api", "acme", "", "prod")
	c.CollapsedGroups = []string{"acme/prod", "prod"}
	c.ActiveSpace = "acme"

	c.RenameSpace("acme", "globex")
	if c.Spaces[1].Name != "globex" || c.Groups[0].Space != "globex" || c.Groups[1].Space != "" || c.Connections["api"].Space != "globex" {
		t.Errorf("after rename: Spaces %+v, Groups %+v, api %+v", c.Spaces, c.Groups, c.Connections["api"])
	}
	if want := []string{"globex/prod", "prod"}; !reflect.DeepEqual(c.CollapsedGroups, want) || c.ActiveSpace != "globex" {
		t.Errorf("collapsed %v, active %q; want %v, globex", c.CollapsedGroups, c.ActiveSpace, want)
	}

	c.RenameSpace("", "home")
	if ref, ok := c.SpaceRef("home"); !ok || ref != "" || !c.Spaces[0].Default {
		t.Errorf("renamed default Space: ref %q %v, Spaces %+v; want it still the default", ref, ok, c.Spaces)
	}
}

func TestSpaceRefResolvesTheDefaultSpaceByNameOrEmpty(t *testing.T) {
	c := New()
	c.Spaces = append(c.Spaces, Space{Name: "acme"})
	for name, want := range map[string]string{"": "", DefaultSpaceName: "", "acme": "acme"} {
		if got, ok := c.SpaceRef(name); !ok || got != want {
			t.Errorf("SpaceRef(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := c.SpaceRef("nowhere"); ok {
		t.Error("SpaceRef found a Space no record has")
	}
}

func TestMetadataWithOnlyASpaceIsKept(t *testing.T) {
	c := New()
	c.Spaces = append(c.Spaces, Space{Name: "acme"})
	c.SetMetadata("api", "acme", "", "")
	if _, ok := c.Connections["api"]; !ok {
		t.Fatal("a Connection in a Space other than the default lost its metadata")
	}
	c.SetMetadata("api", "", "", "")
	if _, ok := c.Connections["api"]; ok {
		t.Error("a Connection in the default Space with nothing else kept metadata")
	}
}

func TestLoadForgetsAnActiveSpaceThatIsGone(t *testing.T) {
	path := writeConfig(t, `{"version":4,"spaces":[{"name":"default","default":true,"number":1}],"connections":{}}`)
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.ActiveSpace = "acme" // as if another machine had deleted acme
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if c, err = Load(path, nil); err != nil {
		t.Fatal(err)
	}
	if c.ActiveSpace != "" {
		t.Errorf("ActiveSpace = %q, want the default Space", c.ActiveSpace)
	}
}

func TestCheckSpaceName(t *testing.T) {
	for _, name := range []string{"", "42", "acme/prod"} {
		if err := CheckSpaceName(name); !errors.Is(err, ErrSpaceName) {
			t.Errorf("CheckSpaceName(%q) = %v, want ErrSpaceName", name, err)
		}
	}
	for _, name := range []string{"acme", "customer1", "prive"} {
		if err := CheckSpaceName(name); err != nil {
			t.Errorf("CheckSpaceName(%q) = %v", name, err)
		}
	}
}

// spaceNumbers lists c's Spaces as name:number, in order.
func spaceNumbers(c *Config) []string {
	var got []string
	for _, s := range c.Spaces {
		got = append(got, fmt.Sprintf("%s:%d", s.Name, s.Number))
	}
	return got
}

func TestLoadGivesEverySpaceAFixedNumber(t *testing.T) {
	for _, tc := range []struct {
		name, spaces string
		want         []string
	}{
		{"numbers 1 to N in their order",
			`[{"name":"home","default":true},{"name":"acme"},{"name":"lab"}]`,
			[]string{"home:1", "acme:2", "lab:3"}},
		{"keeps numbers and gaps, sorted by number",
			`[{"name":"home","default":true,"number":4},{"name":"acme","number":1},{"name":"lab","number":9}]`,
			[]string{"acme:1", "home:4", "lab:9"}},
		{"a repeated or out-of-range number takes the lowest free one",
			`[{"name":"home","default":true,"number":1},{"name":"acme","number":1},{"name":"lab","number":12}]`,
			[]string{"home:1", "acme:2", "lab:3"}},
		{"past nine Spaces the rest have no number",
			`[{"name":"s1","default":true},{"name":"s2"},{"name":"s3"},{"name":"s4"},{"name":"s5"},
			{"name":"s6"},{"name":"s7"},{"name":"s8"},{"name":"s9"},{"name":"s10"},{"name":"s11"}]`,
			[]string{"s1:1", "s2:2", "s3:3", "s4:4", "s5:5", "s6:6", "s7:7", "s8:8", "s9:9", "s10:0", "s11:0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, `{"version":4,"spaces":`+tc.spaces+`,"connections":{}}`)
			c, err := Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := spaceNumbers(c); !slices.Equal(got, tc.want) {
				t.Errorf("Spaces = %v, want %v", got, tc.want)
			}
			again, err := Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := spaceNumbers(again); !slices.Equal(got, tc.want) {
				t.Errorf("reloaded Spaces = %v, want them saved as %v", got, tc.want)
			}
		})
	}
}
