package workspace

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
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

// An action carries out a command for msg, the key that was pressed.
type action func(m *Model, msg tea.KeyPressMsg) tea.Cmd

// A boundKey is one key of a binding and what it does, so a binding whose
// keys differ (up and down, add and edit) needs no second look at the key.
type boundKey struct {
	key string
	run action
}

// A binding is one Workspace command: the keys that run it in each context,
// and its help text. Key handling and the help screen both read bindings.
type binding struct {
	keys [numContexts][]boundKey
	help string
}

// bindings lists every Workspace key binding, in help-screen order.
var bindings = []binding{
	{
		keys: each(inList, on("up", moveCursor(-1)), on("down", moveCursor(1)), on("k", moveCursor(-1)), on("j", moveCursor(1))),
		help: "move the cursor",
	},
	{
		keys: keysIn(inList, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.enter() }, "enter", "space"),
		help: "open or switch to a connection, or toggle a group",
	},
	{
		keys: each(inList, on("left", collapse(true)), on("right", collapse(false)), on("h", collapse(true)), on("l", collapse(false))),
		help: "on a group: collapse / expand it",
	},
	{
		keys: each(inList, on("shift+up", shiftGroup(-1)), on("shift+down", shiftGroup(1)), on("K", shiftGroup(-1)), on("J", shiftGroup(1))),
		help: "on a group: move it up / down (or drag its header)",
	},
	{
		keys: keysIn(inList, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.startFilter(); return nil }, "/"),
		help: "filter the connection list",
	},
	{
		keys: keysIn(inList, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.st.SetFilter(""); return nil }, "esc"),
		help: "clear the filter",
	},
	{
		keys: merge(
			each(inAny, on("alt+left", selectTab(-1)), on("alt+right", selectTab(1))),
			each(inPrefix, on("left", selectTab(-1)), on("right", selectTab(1))),
		),
		help: "previous / next tab",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			m.st.SelectTabIndex(int(msg.String()[0] - '1'))
			m.scrolling = false
			return nil
		}, "1", "2", "3", "4", "5", "6", "7", "8", "9"),
		help: "switch to tab 1-9",
	},
	{
		keys: each(inAny, on("alt+up", cycleActive(-1)), on("alt+down", cycleActive(1))),
		help: "previous / next open connection",
	},
	{
		keys: each(inPrefix, on("n", newTab(false)), on("c", newTab(true))),
		help: "new tab for the active connection (c runs claude)",
	},
	{
		// Each sshctl Action on a Connection runs in its own labelled tab.
		keys: each(inPrefix, on("i", actionTab(ActionBootstrap)), on("p", provisionTab)),
		help: "bootstrap / provision the connection in its own tab",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.openGroup() }, "g"),
		help: "open the connection's group as tabs",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.startMove() }, "m"),
		help: "move a connection to a group (or drag it)",
	},
	{
		keys: merge(each(inList, crud...), each(inPrefix, crud...)),
		help: "add / edit / delete the connection or group under the cursor",
	},
	{
		// Folded into one line so the help screen fits 80x24.
		keys: merge(keysIn(inPrefix, closeTab, "w"), each(inEnded, on("x", closeTab),
			on("r", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reconnect() }))),
		help: "close tab (on an ended tab: x close, r reconnect)",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if m.focus == focusList && m.st.Active() != nil {
				m.focus = focusPane
			} else {
				m.focus = focusList
			}
			return nil
		}, "tab"),
		help: "move focus between list and terminal",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if _, r := m.currentRunner(); r != nil {
				m.scrolling = true
				m.focus = focusPane
			}
			return nil
		}, "["),
		help: "scroll back (mouse wheel also scrolls)",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.hideList = !m.hideList; return nil }, "b"),
		help: "hide or show the connection list",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reload("", "connection list refreshed") }, "r"),
		help: "refresh connections from ssh_config",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if r := m.liveRunner(); r != nil && m.focus == focusPane {
				r.term.SendKey(msg)
			}
			return nil
		}, prefixSelf),
		help: "send the prefix key to the session",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.openModal(&helpScreen{}); return nil }, "?"),
		help: "show this help",
	},
	{
		keys: merge(keysIn(inList, quit, "q", "ctrl+c"), keysIn(inPrefix, quit, "q")),
		help: "quit",
	},
}

// crud is the add, edit and delete keys, bound alike in two contexts. They
// act on the Group whose header is under the cursor, otherwise on a
// Connection.
var crud = []boundKey{
	on("a", func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if _, ok := m.headerTarget(); ok {
			m.openModal(&groupInput{})
			return nil
		}
		return m.startAdd()
	}),
	on("e", func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if g, ok := m.headerTarget(); ok {
			return m.startRenameGroup(g)
		}
		return m.startEdit()
	}),
	on("d", func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if g, ok := m.headerTarget(); ok {
			return m.deleteGroup(g)
		}
		return m.deleteConnection()
	}),
}

func closeTab(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.closeTab() }
func quit(m *Model, _ tea.KeyPressMsg) tea.Cmd     { return m.quit() }

func moveCursor(delta int) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.st.MoveCursor(delta); return nil }
}

func shiftGroup(delta int) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.shiftGroup(delta) }
}

func selectTab(delta int) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.selectTab(delta); return nil }
}

func cycleActive(delta int) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		m.st.CycleActive(delta)
		m.scrolling = false
		return nil
	}
}

// collapse collapses the group under the cursor (from a header or one of its
// members), or expands it from its header.
func collapse(collapsed bool) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		r, ok := m.st.CursorRow()
		switch {
		case !ok:
		case collapsed:
			if m.st.grouped() {
				m.st.SetCursorGroup(r.Group)
				return m.setCollapsed(r.Group, true)
			}
		case r.Header():
			return m.setCollapsed(r.Group, false)
		}
		return nil
	}
}

// newTab opens a Session for the Active connection, or the one under the
// cursor, running claude when claude is set.
func newTab(claude bool) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		target := m.st.Active()
		if target == nil {
			target = m.st.Cursor()
		}
		if target == nil {
			return nil
		}
		return m.open(target.Alias, Launch{Claude: claude})
	}
}

// actionTab runs a in a new tab of the Active connection, or of the one
// under the cursor while the list has focus.
func actionTab(a Action) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		target := m.moveTarget()
		if target == nil {
			return m.setStatus(fmt.Sprintf("no connection to %s", a), false)
		}
		return m.open(target.Alias, Launch{Action: a})
	}
}

// provisionTab asks before running Provision in a new tab of the Connection
// actionTab would pick. A dotfiles setting of none refuses at once, with no
// tab; with no repo set, the tab itself asks for one.
func provisionTab(m *Model, _ tea.KeyPressMsg) tea.Cmd {
	target := m.moveTarget()
	if target == nil {
		return m.setStatus(fmt.Sprintf("no connection to %s", ActionProvision), false)
	}
	alias := target.Alias
	if _, err := m.opts.Catalog.ProvisionRepo(alias); err != nil && !errors.Is(err, catalog.ErrNoDotfiles) {
		return m.setStatus(alias+": "+err.Error(), true)
	}
	m.ask(fmt.Sprintf("Provision %s with your dotfiles? y/n", alias), func(m *Model) tea.Cmd {
		return m.open(alias, Launch{Action: ActionProvision})
	})
	return nil
}

func on(key string, run action) boundKey { return boundKey{key: key, run: run} }

// keysIn binds keys in ctx, all to run.
func keysIn(ctx keyContext, run action, keys ...string) (out [numContexts][]boundKey) {
	for _, k := range keys {
		out[ctx] = append(out[ctx], on(k, run))
	}
	return out
}

// each binds keys in ctx, each to its own action.
func each(ctx keyContext, keys ...boundKey) (out [numContexts][]boundKey) {
	out[ctx] = keys
	return out
}

func merge(a, b [numContexts][]boundKey) [numContexts][]boundKey {
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
		for _, bk := range b.keys[ctx] {
			if bk.key == k {
				return bk.run(m, msg), true
			}
		}
	}
	return nil, false
}

// bindingLabel is b's keys as the help screen shows them, one group per context.
func (m *Model) bindingLabel(b binding) string {
	var parts []string
	for ctx, bound := range b.keys {
		if len(bound) == 0 {
			continue
		}
		keys := make([]string, len(bound))
		for i, bk := range bound {
			keys[i] = bk.key
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
		// A Shift modifier shared with the key before is written once.
		if i > 0 && strings.HasPrefix(k, "shift+") && strings.HasPrefix(keys[i-1], "shift+") {
			names[i] = strings.TrimPrefix(names[i], "Shift+")
		}
	}
	return strings.Join(names, "/")
}

var keyNameReplacer = strings.NewReplacer(
	"left", "←", "right", "→", "up", "↑", "down", "↓",
	"alt+", "Alt+", "shift+", "Shift+", "enter", "Enter", "space", "Space", "esc", "Esc", "tab", "Tab",
)

func keyName(k string) string {
	if strings.HasPrefix(k, "ctrl+") {
		return keyHint(k)
	}
	return keyNameReplacer.Replace(k)
}
