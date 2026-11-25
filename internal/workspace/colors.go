package workspace

import (
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

// indicatorColors are the terminal colours of the Indicator colors
// (ADR-0009): basic ANSI ones, so they follow the user's theme.
var indicatorColors = map[string]color.Color{
	"red":     lipgloss.Red,
	"yellow":  lipgloss.Yellow,
	"green":   lipgloss.Green,
	"cyan":    lipgloss.Cyan,
	"blue":    lipgloss.Blue,
	"magenta": lipgloss.Magenta,
}

// indicator returns base drawn in the Indicator color name, and false
// with base unchanged when name is none or unknown.
func indicator(name string, base lipgloss.Style) (lipgloss.Style, bool) {
	c, ok := indicatorColors[name]
	if !ok {
		return base, false
	}
	return base.Foreground(c), true
}

// frameStyle draws the pane's frame, the line between the list and the
// pane and the rules above and below them: in the Active connection's
// Indicator color, or dim when it has none.
func (m *Model) frameStyle() lipgloss.Style {
	if s, ok := indicator(m.st.ActiveColor(), lipgloss.NewStyle()); ok {
		return s
	}
	return dimStyle
}

// colorChoices are the choices of the color prompt: none (""), then every
// Indicator color.
var colorChoices = append([]string{""}, catalog.Colors...)

// colorTarget returns what C colors: the Group whose header is under the
// cursor, or the Group of the Connection under it, while the list has focus,
// or else the Active connection's Group; an ungrouped Connection, or the
// ungrouped header, gives its Space (group ""), as does an empty list.
func (m *Model) colorTarget() (space, group string) {
	if m.paneFocused() {
		c := m.st.Active()
		return c.Space, c.Group
	}
	r, ok := m.st.CursorRow()
	switch {
	case !ok:
		return m.st.Space(), ""
	case r.Header():
		return m.st.Space(), r.Group
	}
	return r.Conn.Space, r.Conn.Group
}

// colorKey opens the color prompt for colorTarget.
func colorKey(m *Model, _ tea.KeyPressMsg) tea.Cmd {
	space, group := m.colorTarget()
	in := &colorInput{space: space, group: group, name: group}
	if group == "" {
		in.name = m.st.SpaceName(space)
	}
	in.cursor = max(0, slices.Index(colorChoices, m.st.OwnColor(space, group)))
	m.openModal(in)
	return nil
}

// colorInput is the status-line prompt that sets the Indicator color of a
// Group, or of a Space when group is "": none or one of catalog.Colors,
// chosen with ←/→ from the one it has of its own and set with Enter. Asked from the Space picker, back is
// the picker, which Enter and Esc return to.
type colorInput struct {
	prefixGate
	space, group string
	// name is the Group's or Space's name, for the prompt.
	name   string
	cursor int // index into colorChoices
	back   *spacePicker
}

func (in *colorInput) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := in.intercept(m, msg); ok {
		return cmd
	}
	if bk, ok := findKey(in.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	return nil
}

func (in *colorInput) keys() []boundKey {
	move := func(delta int) action {
		return func(*Model, tea.KeyPressMsg) tea.Cmd {
			in.cursor = (in.cursor + delta + len(colorChoices)) % len(colorChoices)
			return nil
		}
	}
	var back modal
	if in.back != nil {
		back = in.back
	}
	return append([]boundKey{
		on("left", move(-1)).as("choose"), on("right", move(1)).as("choose"),
		on("h", move(-1)), on("l", move(1)),
	}, promptKeys("set", func(m *Model) tea.Cmd { return m.saveColor(in) }, back)...)
}

func (in *colorInput) hints() []hintLine {
	if in.swallowed {
		return []hintLine{hintsFor(in.keys(), "esc")}
	}
	return []hintLine{hintsFor(in.keys(), "enter", "esc")}
}

func (in *colorInput) paste(string) {}

// status is the prompt, such as "group web: none red yellow …", each
// choice in its own colour and the cursor's in reverse video.
func (in *colorInput) status() (string, bool) {
	kind := "group "
	if in.group == "" {
		kind = "space "
	}
	choices := make([]string, len(colorChoices))
	for i, c := range colorChoices {
		label := c
		if c == "" {
			label = catalog.ColorNone
		}
		style, _ := indicator(c, lipgloss.NewStyle())
		if i == in.cursor {
			style = style.Reverse(true)
		}
		choices[i] = style.Render(label)
	}
	return kind + in.name + ": " + strings.Join(choices, " ") + "  " + in.hints()[0].String(), true
}

// saveColor saves the color under in's cursor, then closes the prompt, or
// returns to the picker it was asked from.
func (m *Model) saveColor(in *colorInput) tea.Cmd {
	color := colorChoices[in.cursor]
	var err error
	if in.group == "" {
		err = m.opts.Catalog.SetSpaceColor(in.name, color)
	} else {
		err = m.opts.Catalog.SetGroupColor(in.space, in.group, color)
	}
	what := "group " + in.group
	if in.group == "" {
		what = "space " + in.name
	}
	if in.back != nil {
		m.showModal(in.back)
	} else {
		m.showModal(nil)
	}
	if err != nil {
		if in.back != nil {
			in.back.say("color "+what+": "+err.Error(), true)
			return nil
		}
		return m.setStatus("color "+what+": "+err.Error(), true)
	}
	status := what + " color " + color
	if color == "" {
		status = what + " color none"
	}
	if in.back != nil {
		cmd := m.reload("", "")
		in.back.say(status, false)
		return cmd
	}
	return m.reload("", status)
}
