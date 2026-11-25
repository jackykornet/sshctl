package workspace

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// colorMask draws screen, a rendered Workspace, as one letter per column
// for the basic ANSI foreground colour of what is drawn there: k r g y b m
// c w for black to white, . for the default or any other colour, a blank
// where nothing is drawn, and the letter upper case (# for the default)
// where the cell is in reverse video, so the colour fills the cell.
func colorMask(screen string) string {
	const letters = "krgybmcw"
	var out strings.Builder
	for i, line := range strings.Split(screen, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		fg, reverse := -1, false
		for len(line) > 0 {
			if strings.HasPrefix(line, "\x1b[") {
				end := strings.IndexFunc(line[2:], func(r rune) bool { return r >= 0x40 && r <= 0x7e })
				seq := line[2 : 2+end]
				if line[2+end] == 'm' {
					params := strings.Split(seq, ";")
					for j := 0; j < len(params); j++ {
						n, _ := strconv.Atoi(params[j])
						switch {
						case n == 0:
							fg, reverse = -1, false
						case n == 7:
							reverse = true
						case n == 27:
							reverse = false
						case n >= 30 && n <= 37:
							fg = n - 30
						case n == 39 || n >= 90 && n <= 97:
							fg = -1
						case n == 38 || n == 48:
							j += 2 // 38;5;n: not a basic colour
							if n == 38 {
								fg = -1
							}
						}
					}
				}
				line = line[2+end+1:]
				continue
			}
			g, w := ansi.FirstGraphemeCluster(line, ansi.GraphemeWidth)
			line = line[len(g):]
			c := byte('.')
			if fg >= 0 {
				c = letters[fg]
			}
			switch {
			case reverse && fg < 0:
				c = '#'
			case reverse:
				c -= 'a' - 'A'
			case g == " ":
				c = ' '
			}
			out.WriteString(strings.Repeat(string(c), w))
		}
	}
	return out.String()
}

// colorGolden checks the Workspace's text and its colorMask against
// testdata/name.golden.
func colorGolden(t *testing.T, name string, m *Model) {
	t.Helper()
	screen := m.render()
	compareGolden(t, name, ansi.Strip(screen)+"\n"+colorMask(screen), m.w)
}

func TestColorMask(t *testing.T) {
	got := colorMask("a\x1b[31mbc\x1b[m \x1b[7;32md\x1b[m\x1b[7me\x1b[0m─")
	if want := ".rr G#."; got != want {
		t.Errorf("colorMask = %q, want %q", got, want)
	}
}

// newColorModel is a Workspace whose Group web is red, with Space acme
// blue, its Group data uncoloured, and db-main ungrouped in the uncoloured
// default Space.
func newColorModel(t *testing.T, w, h int) (*Model, *fakeCatalog) {
	t.Helper()
	f := &fakeCatalog{
		entries: []Entry{
			{Alias: "staging", Group: "web"},
			{Alias: "prod-web", Description: "public site", Group: "web"},
			{Alias: "db-main", Description: "postgres"},
			{Alias: "db", Space: "acme", Group: "data"},
			{Alias: "cache", Space: "acme"},
		},
		groups:      []string{"web"},
		spaces:      []Space{{Name: "acme"}},
		spaceGroups: []Group{{Name: "data", Space: "acme"}},
		groupColors: map[string]string{"web": "red"},
		spaceColors: map[string]string{"acme": "blue"},
		identity:    "/home/me/.ssh/id_ed25519",
	}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, f
}

func TestStateResolvesColorGroupThenSpaceThenNone(t *testing.T) {
	m, _ := newColorModel(t, 80, 10)
	for _, tc := range []struct{ space, group, want string }{
		{"", "web", "red"},
		{"", "", ""},
		{"acme", "data", "blue"},
		{"acme", "", "blue"},
	} {
		if got := m.st.Color(tc.space, tc.group); got != tc.want {
			t.Errorf("Color(%q, %q) = %q, want %q", tc.space, tc.group, got, tc.want)
		}
	}
	var colors []string
	for _, s := range m.st.SpaceTabs() {
		colors = append(colors, s.Name+"="+s.Color)
	}
	if want := []string{"default=", "acme=blue"}; !slices.Equal(colors, want) {
		t.Errorf("Space colors = %v, want %v", colors, want)
	}
}

func TestIndicatorColorGolden(t *testing.T) {
	t.Run("a red Group frames the pane", func(t *testing.T) {
		m, _ := newColorModel(t, 80, 10)
		m.st.SetCursor("prod-web")
		press(m, keyEnter)
		colorGolden(t, "color-group", m)
		press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // the list has focus
		colorGolden(t, "color-group-list", m)
	})
	t.Run("a blue Space frames the pane", func(t *testing.T) {
		m, _ := newColorModel(t, 80, 10)
		press(m, text("2")...)
		m.st.SetCursor("db")
		press(m, keyEnter)
		colorGolden(t, "color-space", m)
	})
	t.Run("an uncoloured Connection is unchanged", func(t *testing.T) {
		m, _ := newColorModel(t, 80, 10)
		m.st.SetCursor("db-main")
		press(m, keyEnter)
		colorGolden(t, "color-none", m)
	})
}

func TestCSetsTheColorOfAGroupOrItsSpace(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func(m *Model)
		keys  []tea.KeyPressMsg
		want  string
		color func(m *Model) string
	}{
		{"bare on a Group header starts on its color", func(m *Model) { m.st.SetCursorGroup("web") },
			[]tea.KeyPressMsg{text("C")[0], keyRight, keyEnter}, "group web yellow",
			func(m *Model) string { return m.st.Color("", "web") }},
		{"bare on an ungrouped Connection colors its Space", func(m *Model) { m.st.SetCursor("db-main") },
			[]tea.KeyPressMsg{text("C")[0], keyRight, keyEnter}, "space default red",
			func(m *Model) string { return m.st.Color("", "") }},
		{"after the prefix from the pane colors the Active connection's Group", func(m *Model) {
			m.st.SetCursor("prod-web")
			press(m, keyEnter)
			m.st.SetCursor("db-main") // the pane, not the cursor, decides
		}, []tea.KeyPressMsg{keyPrefix, text("C")[0], keyLeft, keyEnter}, "group web none",
			func(m *Model) string { return m.st.ActiveColor() }},
		{"in the Space picker colors the Space under the cursor", func(m *Model) { press(m, text("P")...) },
			[]tea.KeyPressMsg{keyDown, text("C")[0], keyRight, keyEnter}, "space acme magenta",
			func(m *Model) string { return m.st.Color("acme", "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := newColorModel(t, 100, 12)
			tc.open(m)
			press(m, tc.keys...)
			if !slices.Equal(f.colorOps, []string{tc.want}) {
				t.Errorf("color calls = %q, want %q", f.colorOps, tc.want)
			}
			want := strings.Fields(tc.want)[2]
			if want == "none" {
				want = ""
			}
			if got := tc.color(m); got != want {
				t.Errorf("color after reload = %q, want %q", got, want)
			}
		})
	}
}

func TestColorPromptReturnsToThePickerAndEscChangesNothing(t *testing.T) {
	m, f := newColorModel(t, 100, 12)
	press(m, text("P")...)
	press(m, text("C")...)
	if _, ok := m.mode.modal.(*colorInput); !ok {
		t.Fatalf("modal = %T, want the color prompt", m.mode.modal)
	}
	press(m, keyRight, keyEsc)
	if m.picker() == nil || len(f.colorOps) != 0 {
		t.Errorf("Esc: modal %T, color calls %q; want the picker and none", m.mode.modal, f.colorOps)
	}
	press(m, keyEsc)
	m.st.SetCursorGroup("web")
	press(m, text("C")...)
	press(m, keyEsc)
	if m.mode.modal != nil || len(f.colorOps) != 0 {
		t.Errorf("Esc: modal %T, color calls %q; want none", m.mode.modal, f.colorOps)
	}
}

func TestColorPromptGolden(t *testing.T) {
	m, _ := newColorModel(t, 80, 10)
	m.st.SetCursorGroup("web")
	press(m, text("C")...)
	golden(t, "color-prompt", m)
	if hint := m.mode.modal.(hinter).hints()[0].String(); !strings.HasPrefix(hint, "←/→ choose · ") {
		t.Errorf("color prompt hints = %q, want ←/→ choose first", hint)
	}
}
