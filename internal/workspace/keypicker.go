package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// keyPicker lists the keys a Selected identity is chosen from (the ones
// "sshctl key list" shows, by private key) after "OpenSSH default", with a
// cursor. Enter selects the key under it for the active Space, g for every
// Space without its own. Both go through the catalog, as "sshctl key use"
// does, and apply to tabs opened afterwards: open Sessions keep the key
// they started with.
type keyPicker struct {
	prefixGate
	// rows are private key paths, "" (OpenSSH default) first.
	rows   []string
	cursor int
}

// openKeyPicker opens the key picker with its cursor on the key the active
// Space uses.
func (m *Model) openKeyPicker() tea.Cmd {
	files, err := m.opts.Catalog.Keys()
	if err != nil {
		return m.setStatus("keys: "+err.Error(), true)
	}
	p := &keyPicker{}
	for _, f := range files {
		if k := privateKey(f); !slices.Contains(p.rows, k) {
			p.rows = append(p.rows, k)
		}
	}
	// A Selected identity without a public key in the list still shows.
	for _, k := range []string{m.identities.Identity, m.identities.SpaceIdentities[m.st.Space()]} {
		if k != "" && !slices.Contains(p.rows, k) {
			p.rows = append(p.rows, k)
		}
	}
	slices.Sort(p.rows)
	p.rows = append([]string{""}, p.rows...)
	p.cursor = max(0, slices.Index(p.rows, m.identities.SpaceIdentity(m.st.Space())))
	m.openModal(p)
	return nil
}

// privateKey is the private key a public key or certificate file in the
// key list belongs to; ssh loads the certificate beside it on its own.
func privateKey(file string) string {
	if k, ok := strings.CutSuffix(file, "-cert.pub"); ok {
		return k
	}
	return strings.TrimSuffix(file, ".pub")
}

func (p *keyPicker) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := p.intercept(m, msg); ok {
		return cmd
	}
	if bk, ok := findKey(p.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	return nil
}

func (p *keyPicker) keys() []boundKey {
	move := func(delta int) action {
		return movePickerCursor(&p.cursor, delta, func(*Model) int { return len(p.rows) })
	}
	forSpace := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		space := m.st.ActiveSpaceName()
		return m.selectKey(func() error { return m.opts.Catalog.SetSpaceIdentity(space, p.rows[p.cursor]) }, "space "+space)
	}
	global := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		return m.selectKey(func() error { return m.opts.Catalog.SetIdentity(p.rows[p.cursor]) }, "global key")
	}
	return []boundKey{
		on("enter", forSpace).as("use in space"), on("space", forSpace),
		on("g", global).as("use globally"),
		on("up", move(-1)), on("k", move(-1)),
		on("down", move(1)), on("j", move(1)),
		on("esc", closeModal).as("close"), on("ctrl+c", closeModal), on("q", closeModal), on("I", closeModal),
	}
}

// selectKey closes the picker and saves a Selected identity with set, then
// reloads and says which key the active Space's new tabs now use, or why
// set failed (what names the change).
func (m *Model) selectKey(set func() error, what string) tea.Cmd {
	m.showModal(nil)
	if err := set(); err != nil {
		return m.setStatus(what+": "+err.Error(), true)
	}
	if cmd := m.reload("", ""); m.statusErr {
		return cmd
	}
	k := keyLabel(m.identities.SpaceIdentity(m.st.Space()))
	return m.setStatus("new tabs in space "+m.st.ActiveSpaceName()+" use "+k+"; open tabs keep their key", false)
}

func (p *keyPicker) hints() []hintLine {
	if p.swallowed {
		return []hintLine{hintsFor(p.keys(), "esc")}
	}
	return []hintLine{hintsFor(p.keys(), "enter", "g", "esc")}
}

func (p *keyPicker) paste(string) {}

func (p *keyPicker) status() (string, bool) {
	if p.swallowed {
		return "key picker open  " + p.hints()[0].String(), true
	}
	return p.hints()[0].String(), true
}

// keyLabel names the Selected identity k on the status line.
func keyLabel(k string) string {
	if k == "" {
		return "OpenSSH default"
	}
	return filepath.Base(k)
}

// render draws the picker in the w by h pane: a row per key, ● on the one
// the active Space's new tabs use, tagged with the Space when it is the
// Space's own and with global when it is the global one.
func (p *keyPicker) render(m *Model, w, h int) []string {
	space := m.st.Space()
	lines := []string{
		fit(" "+boldStyle.Render("Keys for space "+m.st.ActiveSpaceName()), w),
		fit(" "+dimStyle.Render("New tabs only: open tabs keep their key."), w),
	}
	inUse := m.identities.SpaceIdentity(space)
	own, hasOwn := m.identities.SpaceIdentities[space]
	home, _ := os.UserHomeDir()
	for i, k := range p.rows {
		pointer := "  "
		if i == p.cursor {
			pointer = " ›"
		}
		mark := " "
		if k == inUse {
			mark = "●"
		}
		label := "OpenSSH default"
		if k != "" {
			label = k
			if rest, ok := strings.CutPrefix(k, home+string(filepath.Separator)); ok && home != "" {
				label = "~" + string(filepath.Separator) + rest
			}
		}
		var tags []string
		if hasOwn && own == k {
			tags = append(tags, "space "+m.st.ActiveSpaceName())
		}
		if m.identities.Identity == k {
			tags = append(tags, "global")
		}
		row := pointer + " " + mark + " " + label
		if len(tags) > 0 {
			row += "  " + dimStyle.Render(strings.Join(tags, ", "))
		}
		lines = append(lines, fit(row, w))
	}
	return padLines(lines, w, h)
}
