package workspace

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// keyContext is where a binding's keys apply. Modals take their own keys
// before any of these; see modal.
type keyContext int

const (
	// inAny is outside modals with either focus, even while filtering.
	inAny keyContext = iota
	// inList is the connection list with focus and no filter being typed.
	inList
	// inPrefix is the key after the prefix key.
	inPrefix
	// inEnded is the terminal pane with focus on an ended Session.
	inEnded
	numContexts
)

// prefixSelf stands for the prefix key itself in a binding's keys.
const prefixSelf = "<prefix>"

// A binding is one Workspace command: the keys that run it in each context,
// and its help text. Key handling and the help screen both read bindings.
type binding struct {
	keys [numContexts][]string
	help string
	// run carries out the command for msg, a key the binding has in the
	// context it was pressed in.
	run func(m *Model, msg tea.KeyPressMsg) tea.Cmd
}

// bindings lists every Workspace key binding, in help-screen order.
var bindings = []binding{
	{
		keys: keysIn(inList, "up", "down", "k", "j"),
		help: "move the cursor",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			if k == "up" || k == "k" {
				m.st.MoveCursor(-1)
			} else {
				m.st.MoveCursor(1)
			}
			return nil
		},
	},
	{
		keys: keysIn(inList, "enter", "space"),
		help: "open or switch to a connection, or toggle a group",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.enter() },
	},
	{
		keys: keysIn(inList, "left", "right", "h", "l"),
		help: "on a group: collapse / expand it",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			r, ok := m.st.CursorRow()
			switch {
			case !ok:
			case k == "left" || k == "h":
				if m.st.grouped() {
					m.st.SetCursorGroup(r.Group)
					return m.setCollapsed(r.Group, true)
				}
			case r.Header():
				return m.setCollapsed(r.Group, false)
			}
			return nil
		},
	},
	{
		keys: keysIn(inList, "/"),
		help: "filter the connection list",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.filtering = true; return nil },
	},
	{
		keys: keysIn(inList, "esc"),
		help: "clear the filter",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.st.SetFilter(""); return nil },
	},
	{
		keys: merge(keysIn(inAny, "alt+left", "alt+right"), keysIn(inPrefix, "left", "right")),
		help: "previous / next tab",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			if strings.HasSuffix(k, "left") {
				m.selectTab(-1)
			} else {
				m.selectTab(1)
			}
			return nil
		},
	},
	{
		keys: keysIn(inPrefix, "1", "2", "3", "4", "5", "6", "7", "8", "9"),
		help: "switch to tab 1-9",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			m.st.SelectTabIndex(int(k[0] - '1'))
			m.scrolling = false
			return nil
		},
	},
	{
		keys: keysIn(inAny, "alt+up", "alt+down"),
		help: "previous / next open connection",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			if k == "alt+up" {
				m.st.CycleActive(-1)
			} else {
				m.st.CycleActive(1)
			}
			m.scrolling = false
			return nil
		},
	},
	{
		keys: keysIn(inPrefix, "n", "c"),
		help: "new tab for the active connection (c runs claude)",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			target := m.st.Active()
			if target == nil {
				target = m.st.Cursor()
			}
			if target == nil {
				return nil
			}
			return m.open(target.Alias, k == "c")
		},
	},
	{
		keys: keysIn(inPrefix, "g"),
		help: "open the connection's group as tabs",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.openGroup() },
	},
	{
		keys: keysIn(inPrefix, "m"),
		help: "move a connection to a group (or drag it)",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.startMove() },
	},
	{
		keys: merge(keysIn(inList, "a", "e", "d"), keysIn(inPrefix, "a", "e", "d")),
		help: "add / edit / delete a connection",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			k := msg.String()
			switch k {
			case "a":
				return m.startAdd()
			case "e":
				return m.startEdit()
			}
			return m.deleteConnection()
		},
	},
	{
		keys: merge(keysIn(inPrefix, "w"), keysIn(inEnded, "x")),
		help: "close tab (x on an ended tab)",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.closeTab() },
	},
	{
		keys: keysIn(inEnded, "r"),
		help: "on an ended tab: reconnect",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reconnect() },
	},
	{
		keys: keysIn(inPrefix, "tab"),
		help: "move focus between list and terminal",
		run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if m.focus == focusList && m.st.Active() != nil {
				m.focus = focusPane
			} else {
				m.focus = focusList
			}
			return nil
		},
	},
	{
		keys: keysIn(inPrefix, "["),
		help: "scroll back (mouse wheel also scrolls)",
		run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if _, r := m.currentRunner(); r != nil {
				m.scrolling = true
				m.focus = focusPane
			}
			return nil
		},
	},
	{
		keys: keysIn(inPrefix, "b"),
		help: "hide or show the connection list",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.hideList = !m.hideList; return nil },
	},
	{
		keys: keysIn(inPrefix, "r"),
		help: "refresh connections from ssh_config",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reload("", "connection list refreshed") },
	},
	{
		keys: keysIn(inPrefix, prefixSelf),
		help: "send the prefix key to the session",
		run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if r := m.liveRunner(); r != nil && m.focus == focusPane {
				r.term.SendKey(msg)
			}
			return nil
		},
	},
	{
		keys: keysIn(inPrefix, "?"),
		help: "show this help",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.openModal(helpScreen{}); return nil },
	},
	{
		keys: merge(keysIn(inList, "q", "ctrl+c"), keysIn(inPrefix, "q")),
		help: "quit",
		run:  func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.quit() },
	},
}

func keysIn(ctx keyContext, keys ...string) (out [numContexts][]string) {
	out[ctx] = keys
	return out
}

func merge(a, b [numContexts][]string) [numContexts][]string {
	for i := range a {
		a[i] = append(a[i], b[i]...)
	}
	return a
}

// runBinding runs the binding for msg in ctx, reporting false when there is none.
func (m *Model) runBinding(ctx keyContext, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if ctx == inPrefix && k == m.opts.PrefixKey {
		k = prefixSelf
	}
	for _, b := range bindings {
		if slices.Contains(b.keys[ctx], k) {
			return b.run(m, msg), true
		}
	}
	return nil, false
}

// bindingLabel is b's keys as the help screen shows them, one group per context.
func (m *Model) bindingLabel(b binding) string {
	var parts []string
	for ctx, keys := range b.keys {
		if len(keys) == 0 {
			continue
		}
		label := keyNames(keys, m.opts.PrefixKey)
		if keyContext(ctx) == inPrefix {
			label = keyHint(m.opts.PrefixKey) + " " + label
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}

// keyNames shows keys joined by "/", with a run of the digits 1 to 9 as
// "1-9" and an Alt modifier they all share written once.
func keyNames(keys []string, prefixKey string) string {
	if slices.Equal(keys, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}) {
		return "1-9"
	}
	if !slices.ContainsFunc(keys, func(k string) bool { return !strings.HasPrefix(k, "alt+") }) {
		bare := make([]string, len(keys))
		for i, k := range keys {
			bare[i] = strings.TrimPrefix(k, "alt+")
		}
		return "Alt+" + keyNames(bare, prefixKey)
	}
	names := make([]string, len(keys))
	for i, k := range keys {
		if k == prefixSelf {
			k = prefixKey
		}
		names[i] = keyName(k)
	}
	return strings.Join(names, "/")
}

var keyNameReplacer = strings.NewReplacer(
	"left", "←", "right", "→", "up", "↑", "down", "↓",
	"alt+", "Alt+", "enter", "Enter", "space", "Space", "esc", "Esc", "tab", "Tab",
)

func keyName(k string) string {
	if strings.HasPrefix(k, "ctrl+") {
		return keyHint(k)
	}
	return keyNameReplacer.Replace(k)
}
