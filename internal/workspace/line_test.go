package workspace

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var (
	keyLeft      = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyRight     = tea.KeyPressMsg{Code: tea.KeyRight}
	keyHome      = tea.KeyPressMsg{Code: tea.KeyHome}
	keyEnd       = tea.KeyPressMsg{Code: tea.KeyEnd}
	keyBackspace = tea.KeyPressMsg{Code: tea.KeyBackspace}
	keyDelete    = tea.KeyPressMsg{Code: tea.KeyDelete}
	keyCtrlA     = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	keyCtrlE     = tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
)

func keys(groups ...any) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, g := range groups {
		switch g := g.(type) {
		case string:
			out = append(out, text(g)...)
		case tea.KeyPressMsg:
			out = append(out, g)
		}
	}
	return out
}

// The cursor is drawn as █ where typed text is inserted.
func TestLineEditing(t *testing.T) {
	tests := []struct {
		name  string
		start string
		keys  []tea.KeyPressMsg
		want  string
	}{
		{"starts at the end", "web1", nil, "web1█"},
		{"left moves back", "web1", keys(keyLeft, keyLeft), "we█b1"},
		{"left stops at the start", "ab", keys(keyLeft, keyLeft, keyLeft), "█ab"},
		{"right stops at the end", "ab", keys(keyLeft, keyRight, keyRight), "ab█"},
		{"typing inserts at the cursor", "web1", keys(keyLeft, keyLeft, "x"), "wex█b1"},
		{"home and end", "web1", keys(keyHome, "x", keyEnd, "y"), "xweb1y█"},
		{"ctrl+a and ctrl+e", "web1", keys(keyCtrlA, "x", keyCtrlE, "y"), "xweb1y█"},
		{"backspace deletes before the cursor", "web1", keys(keyLeft, keyBackspace), "we█1"},
		{"backspace at the start does nothing", "ab", keys(keyHome, keyBackspace), "█ab"},
		{"delete removes after the cursor", "web1", keys(keyLeft, keyLeft, keyDelete), "we█1"},
		{"delete at the end does nothing", "ab", keys(keyDelete), "ab█"},
		{"delete at the start", "ab", keys(keyHome, keyDelete), "█b"},
		{"ctrl+u clears the line", "web1", keys(keyLeft, keyClear), "█"},
		{"multi-byte runes move one at a time", "héllo", keys(keyLeft, keyLeft, keyLeft, keyLeft, keyDelete, "e"), "he█llo"},
		{"empty line", "", keys(keyLeft, keyRight, keyBackspace, keyDelete), "█"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLine(tt.start)
			for _, k := range tt.keys {
				l.key(k)
			}
			if got := l.withCursor(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineReportsOnlyKeysItUses(t *testing.T) {
	l := newLine("ab")
	for _, k := range []tea.KeyPressMsg{keyEnter, keyDown, {Code: tea.KeyTab}, {Code: 's', Mod: tea.ModCtrl}} {
		if l.key(k) {
			t.Errorf("%s was used by the line", k)
		}
	}
	if !l.key(keyLeft) {
		t.Error("left was not used by the line")
	}
}

func TestLinePasteInsertsAtTheCursorWithoutLineBreaks(t *testing.T) {
	l := newLine("web1")
	l.key(keyLeft)
	l.paste("x\ny\tz")
	if got := l.withCursor(); got != "webxyz█1" {
		t.Errorf("got %q, want %q", got, "webxyz█1")
	}
	if got := l.String(); got != "webxyz1" {
		t.Errorf("String() = %q, want %q", got, "webxyz1")
	}
}

// #43: a typo in the connection form is fixed in place.
func TestFormFieldsEditAtTheCursor(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("e")...)
	// Alias "web": fix it to "wxeb", then HostName "10.0.0.1" to "10.9.0.1".
	press(m, keys(keyLeft, keyLeft, "x", keyDown, keyHome, keyRight, keyRight, keyRight, keyDelete, "9")...)
	press(m, keyCtrlS)
	if len(f.specs) != 1 {
		t.Fatalf("saved %d specs, want 1", len(f.specs))
	}
	if got := f.specs[0]; got.Alias != "wxeb" || got.Host.HostName != "10.9.0.1" {
		t.Errorf("saved alias %q hostname %q, want wxeb and 10.9.0.1", got.Alias, got.Host.HostName)
	}
}

func TestGroupPromptEditsAtTheCursor(t *testing.T) {
	m, f := newGroupModel(t)
	press(m, keyPrefix, text("e")[0]) // rename web, prefilled
	press(m, keys(keyHome, "my-", keyEnter)...)
	if want := []string{"rename web→my-web"}; !reflect.DeepEqual(f.groupOps, want) {
		t.Fatalf("group calls = %v, want %v", f.groupOps, want)
	}
}

func TestMovePromptEditsAtTheCursor(t *testing.T) {
	m, f := newGroupModel(t)
	m.st.SetCursor("alpha")
	press(m, keyPrefix, text("m")[0]) // prefilled with alpha's group, web
	press(m, keys(keyHome, "batch", keyDelete, keyDelete, keyDelete, keyEnter)...)
	if want := [][2]string{{"alpha", "batch"}}; !reflect.DeepEqual(f.moves, want) {
		t.Fatalf("saved %v, want %v (status %q)", f.moves, want, m.status)
	}
}

// Left and Right edit the filter instead of collapsing the Group under the
// cursor.
func TestFilterEditsAtTheCursor(t *testing.T) {
	m, _ := newGroupModel(t)
	press(m, text("/aph")...)
	press(m, keys(keyLeft, keyLeft, "l")...)
	if got := m.st.Filter(); got != "alph" {
		t.Fatalf("filter = %q, want alph", got)
	}
	press(m, keyEnter, text("/")[0], keyLeft, keyBackspace)
	if got := m.st.Filter(); got != "alh" {
		t.Errorf("filter = %q, want alh: / resumes with the cursor at the end", got)
	}
}

func TestFormDrawsTheCursorInsideTheValue(t *testing.T) {
	m := newStoreModel(t, &fakeCatalog{entries: storeEntries()})
	m.st.SetCursor("web")
	press(m, text("e")...)
	press(m, keyDown, keyLeft, keyLeft)
	golden(t, "form-cursor", m)
}
