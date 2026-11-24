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
	// inList is the connection list with focus and no filter being typed.
	inList keyContext = iota
	// inPrefix is the key after the prefix key, with either focus.
	inPrefix
	// inEnded is the terminal pane with focus on an ended Session.
	inEnded
	// inTunnels is the connection list with focus and the cursor on a row
	// of the Tunnels section. Its keys come before inList's.
	inTunnels
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
	// name is what the key does in a hint line, such as "close tab"; a key
	// without one is left out of hints.
	name string
}

// as names k for hint lines.
func (k boundKey) as(name string) boundKey { k.name = name; return k }

// A binding is one Workspace command: the keys that run it in each context,
// and its help text. Key handling and the help screen both read bindings.
type binding struct {
	keys [numContexts][]boundKey
	help string
	// features lists the Experimental features the binding runs; the
	// help screen says which of them are off.
	features []string
	// listOnly marks a binding that moves around the list (its cursor,
	// Groups and filter), so it has no prefix keys (ADR-0007).
	listOnly bool
	// tunnelRows lets the binding's inList keys work with the cursor on a
	// row of the Tunnels section; the other inList keys act on a
	// Connection or Group and wait for the cursor to be back on the list.
	tunnelRows bool
}

// bindings lists every Workspace key binding, in help-screen order.
var bindings = []binding{
	{
		keys:       each(inList, on("up", moveCursor(-1)), on("down", moveCursor(1)), on("k", moveCursor(-1)), on("j", moveCursor(1))),
		help:       "move the cursor",
		listOnly:   true,
		tunnelRows: true,
	},
	{
		keys:     keysIn(inList, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.enter() }, "enter", "space"),
		help:     "open or switch to a connection, or toggle a group",
		listOnly: true,
	},
	{
		keys:     each(inList, on("left", collapse(true)), on("right", collapse(false)), on("h", collapse(true)), on("l", collapse(false))),
		help:     "on a group: collapse / expand it",
		listOnly: true,
	},
	{
		keys:     each(inList, on("shift+up", shiftGroup(-1)), on("shift+down", shiftGroup(1)), on("K", shiftGroup(-1)), on("J", shiftGroup(1))),
		help:     "on a group: move it up / down (or drag its header)",
		listOnly: true,
	},
	{
		keys: both(on("/", func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			m.focus = focusList
			m.startFilter()
			return nil
		})),
		help:       "filter the connection list",
		tunnelRows: true,
	},
	{
		keys:       keysIn(inList, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.st.SetFilter(""); return nil }, "esc"),
		help:       "clear the filter",
		listOnly:   true,
		tunnelRows: true,
	},
	{
		keys:       both(on("[", selectTab(-1)).as("tab"), on("]", selectTab(1)).as("tab")),
		help:       "previous / next tab",
		tunnelRows: true,
	},
	{
		keys:       both(on("{", cycleActive(-1)).as("connection"), on("}", cycleActive(1)).as("connection")),
		help:       "previous / next open connection in the Space",
		tunnelRows: true,
	},
	{
		// The picker and the note share the digits' line so the help
		// screen fits 80x24.
		keys: both(append([]boundKey{
			on("P", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.openPicker(); return nil }).as("spaces"),
			on("S", spaceNote),
		}, digitKeys()...)...),
		help:       "Space picker / Space names / switch to Space 1-9",
		tunnelRows: true,
	},
	{
		keys: each(inPrefix, on("n", followCursor(newTab(LaunchShell))).as("new tab"), on("c", followCursor(newTab(LaunchClaude)))),
		help: "new tab for the active connection (c runs claude)",
	},
	{
		// Each sshctl Action on a Connection runs in its own labelled tab.
		keys:     each(inPrefix, on("i", followCursor(bootstrapTab)), on("p", followCursor(provisionTab))),
		help:     "bootstrap / provision in its own tab",
		features: []string{catalog.FeatureBootstrap, catalog.FeatureProvision},
	},
	{
		keys: keysIn(inPrefix, followCursor(func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.openGroup() }), "g"),
		help: "open the connection's group as tabs",
	},
	{
		keys: keysIn(inPrefix, followCursor(func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.startMove() }), "m"),
		help: "move a connection to a group (or drag it)",
	},
	{
		keys:       both(on("t", tunnelKey).as("tunnels")),
		help:       "start / stop the connection's tunnels",
		tunnelRows: true,
	},
	{
		keys: both(crud...),
		help: "add / edit / delete the cursor's connection or group",
	},
	{
		// Folded into one line so the help screen fits 80x24. r and x
		// mean the same on an ended tab and on a tunnel row (ADR-0007):
		// r reconnects the tab or restarts the tunnel, x closes or stops.
		keys: merge(each(inPrefix, on("w", closeTab).as("close")), each(inEnded,
			on("r", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reconnect() }).as("reconnect"),
			on("x", closeTab).as("close")), each(inTunnels,
			on("r", tunnelRowKey((*Model).startTunnel)).as("restart"),
			on("x", tunnelRowKey((*Model).stopTunnel)).as("stop"))),
		help: "close tab (ended tab, tunnel row: r reconnect, x close)",
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
				m.setBase(mode{kind: modeScroll}) // ends typing a filter
				m.focus = focusPane
			}
			return nil
		}, "s"),
		help: "scroll back (mouse wheel also scrolls)",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.hideList = !m.hideList; return nil }, "b"),
		help: "hide or show the connection list",
	},
	{
		keys: keysIn(inPrefix, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reload("", "connection list refreshed") }, "R"),
		help: "refresh connections from ssh_config",
	},
	{
		// Folded into one line so the help screen fits 80x24.
		keys: merge(keysIn(inPrefix, func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if r := m.liveRunner(); r != nil && m.focus == focusPane {
				r.term.SendKey(msg)
			}
			return nil
		}, prefixSelf), each(inPrefix, on("?", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.openModal(&helpScreen{}); return nil }).as("help"))),
		help: "send the prefix key to the session / show this help",
	},
	{
		// Bare Ctrl+C in the pane is the Session's.
		keys:       both(on("q", quit).as("quit"), on("ctrl+c", quit)),
		help:       "quit",
		tunnelRows: true,
	},
}

// both binds keys alike in the list and after the prefix, as ADR-0007 has
// every list action.
func both(keys ...boundKey) [numContexts][]boundKey {
	return merge(each(inList, keys...), each(inPrefix, keys...))
}

// digitKeys switch to Spaces 1 to 9.
func digitKeys() []boundKey {
	out := make([]boundKey, len(digits))
	for i, d := range digits {
		out[i] = on(d, switchSpace)
	}
	return out
}

// crud is the add, edit and delete keys, a list action. They act on the
// Group whose header is under the cursor, otherwise on a Connection.
var crud = []boundKey{
	on("a", followCursor(func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if _, ok := m.headerTarget(); ok {
			m.openModal(&groupInput{})
			return nil
		}
		return m.startAdd()
	})),
	on("e", followCursor(func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if g, ok := m.headerTarget(); ok {
			return m.startRenameGroup(g)
		}
		return m.startEdit()
	})),
	on("d", func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if g, ok := m.headerTarget(); ok {
			return m.deleteGroup(g)
		}
		return m.deleteConnection()
	}),
}

// digits are the keys 1 to 9, for Spaces 1 to 9.
var digits = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}

func switchSpace(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	return m.switchSpace(int(msg.String()[0] - '0'))
}

func spaceNote(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.showSpaceNote() }

func closeTab(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.closeTab() }

// tunnelKey starts or stops the tunnel of the tunnel row under the cursor,
// or else of the Connection a move would pick (see Model.toggleTunnel).
func tunnelKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if alias, ok := m.tunnelTarget(); ok {
		return m.toggleTunnel(alias)
	}
	return followCursor(func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		c := m.moveTarget()
		if c == nil {
			return m.setStatus("no connection for a tunnel", false)
		}
		return m.toggleTunnel(c.Alias)
	})(m, msg)
}

// tunnelRowKey runs run on the alias of the tunnel row under the cursor.
func tunnelRowKey(run func(m *Model, alias string) tea.Cmd) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if alias, ok := m.tunnelTarget(); ok {
			return run(m, alias)
		}
		return nil
	}
}

func quit(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.quit() }

func moveCursor(delta int) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.moveCursor(delta); return nil }
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
		m.stopScroll()
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
// cursor, running kind.
func newTab(kind LaunchKind) action {
	return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		target := m.st.Active()
		if target == nil {
			target = m.st.Cursor()
		}
		if target == nil {
			return nil
		}
		return m.open(target.Alias, Launch{Kind: kind})
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

// bootstrapTab runs Bootstrap in a new tab of the Connection actionTab
// would pick, and refuses at once, with no tab, while Bootstrap is off.
func bootstrapTab(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if err := m.features.Check(catalog.FeatureBootstrap); err != nil {
		return m.setStatus(err.Error(), true)
	}
	return actionTab(ActionBootstrap)(m, msg)
}

// helpText is b's help-screen text, saying which of its Experimental
// features are off.
func (m *Model) helpText(b binding) string {
	var off []string
	for _, f := range b.features {
		if !m.features.On(f) {
			off = append(off, f)
		}
	}
	switch {
	case len(off) == 0:
		return b.help
	case len(off) == len(b.features):
		return b.help + " (experimental, off)"
	}
	return b.help + " (" + strings.Join(off, ", ") + " off)"
}

// provisionTab asks before running Provision in a new tab of the Connection
// actionTab would pick. While Provision is off, or with a dotfiles setting
// of none, it refuses at once, with no tab; with no repo set, the tab
// itself asks for one.
func provisionTab(m *Model, _ tea.KeyPressMsg) tea.Cmd {
	if err := m.features.Check(catalog.FeatureProvision); err != nil {
		return m.setStatus(err.Error(), true)
	}
	target := m.moveTarget()
	if target == nil {
		return m.setStatus(fmt.Sprintf("no connection to %s", ActionProvision), false)
	}
	alias := target.Alias
	if _, err := m.opts.Catalog.ProvisionRepo(alias); err != nil && !errors.Is(err, catalog.ErrNoDotfiles) {
		return m.setStatus(alias+": "+err.Error(), true)
	}
	m.ask(fmt.Sprintf("Provision %s with your dotfiles?", alias), func(m *Model) tea.Cmd {
		return m.open(alias, Launch{Action: ActionProvision})
	})
	return nil
}

// followCursor runs run in the Space of the Connection under the cursor,
// switching to it first when the filter found that Connection in another
// Space (see Model.showSpaceOf). It only looks at the cursor when the
// action would: while the list has focus, or no Connection is active.
func followCursor(run action) action {
	return func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
		var saved tea.Cmd
		if c := m.st.Cursor(); c != nil && (m.focus == focusList || m.st.Active() == nil) {
			saved = m.showSpaceOf(c)
		}
		return tea.Batch(saved, run(m, msg))
	}
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

// merge binds every key of sets, in order.
func merge(sets ...[numContexts][]boundKey) (out [numContexts][]boundKey) {
	for _, set := range sets {
		for i := range out {
			out[i] = append(out[i], set[i]...)
		}
	}
	return out
}

// runBinding runs the binding for msg in ctx, reporting false when there is none.
func (m *Model) runBinding(ctx keyContext, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	_, bk, ok := m.lookupBinding(ctx, msg)
	if !ok {
		return nil, false
	}
	return bk.run(m, msg), true
}

// lookupBinding finds the binding for msg in ctx.
func (m *Model) lookupBinding(ctx keyContext, msg tea.KeyPressMsg) (binding, boundKey, bool) {
	k := msg.String()
	if ctx == inPrefix && k == m.opts.PrefixKey {
		k = prefixSelf
	}
	for _, b := range bindings {
		for _, bk := range b.keys[ctx] {
			if bk.key == k {
				return b, bk, true
			}
		}
	}
	return binding{}, boundKey{}, false
}

// listKey runs msg's binding for the list: with the cursor in the Tunnels
// section, its own keys first, and of the list's only those that work there.
func (m *Model) listKey(msg tea.KeyPressMsg) tea.Cmd {
	if _, ok := m.tunnelTarget(); ok {
		if cmd, ok := m.runBinding(inTunnels, msg); ok {
			return cmd
		}
		if b, bk, ok := m.lookupBinding(inList, msg); ok && b.tunnelRows {
			return bk.run(m, msg)
		}
		return nil
	}
	cmd, _ := m.runBinding(inList, msg)
	return cmd
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
		// The same keys in another context are shown once.
		if slices.Contains(parts, label) {
			continue
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}

// keyNames shows keys joined by "/", with the digits 1 to 9 ending them
// as "1-9" and an Alt modifier they all share written once.
func keyNames(keys []string, prefixKey string) string {
	if rest, ok := cutDigits(keys); ok {
		if len(rest) == 0 {
			return "1-9"
		}
		return keyNames(rest, prefixKey) + "/1-9"
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

// cutDigits returns keys without the digits 1 to 9 at their end, and
// whether they ended so.
func cutDigits(keys []string) ([]string, bool) {
	n := len(keys) - len(digits)
	if n < 0 || !slices.Equal(keys[n:], digits) {
		return keys, false
	}
	return keys[:n], true
}

var keyNameReplacer = strings.NewReplacer(
	"left", "←", "right", "→", "up", "↑", "down", "↓",
	"pgup", "PgUp", "pgdown", "PgDn", "home", "Home", "end", "End",
	"alt+", "Alt+", "shift+", "Shift+", "enter", "Enter", "space", "Space", "esc", "Esc", "tab", "Tab",
)

func keyName(k string) string {
	if strings.HasPrefix(k, "ctrl+") {
		return keyHint(k)
	}
	return keyNameReplacer.Replace(k)
}
