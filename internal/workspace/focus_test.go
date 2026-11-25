package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var keyTab = tea.KeyPressMsg{Code: tea.KeyTab}

// styled is s drawn in style, without the reset after it, so it matches
// text that continues in the same style.
func styled(style lipgloss.Style, s string) string {
	return strings.TrimSuffix(style.Render(s), "\x1b[m")
}

func TestFocusGolden(t *testing.T) {
	t.Run("list focused", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, keyDown, keyEnter) // prod-web
		press(m, keyPrefix, keyTab)
		golden(t, "focus-list", m)
	})
	t.Run("pane focused", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, keyDown, keyEnter)
		golden(t, "focus-pane", m)
	})
	t.Run("list hidden, pane focused", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, keyDown, keyEnter)
		press(m, keyPrefix, text("b")[0])
		golden(t, "focus-hidden-list", m)
	})
	t.Run("narrow terminal", func(t *testing.T) {
		m := newTestModel(t, 40, 6)
		m.st.SetCursor("db-main")
		press(m, keyEnter)
		golden(t, "focus-narrow", m)
	})
	t.Run("no Session", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, keyPrefix, keyTab)
		golden(t, "focus-no-session", m)
	})
}

func TestStatusTagShowsFocus(t *testing.T) {
	m := newTestModel(t, 100, 10)
	if got := strings.TrimSpace(statusLine(m)); !strings.HasPrefix(got, "LIST no active connection") {
		t.Errorf("status = %q, want the LIST tag", got)
	}
	m.st.SetCursor("prod-web")
	press(m, keyEnter)
	if got := strings.TrimSpace(statusLine(m)); !strings.HasPrefix(got, "SESSION web / prod-web") {
		t.Errorf("status = %q, want the SESSION tag", got)
	}
	press(m, keyPrefix)
	if got := strings.TrimSpace(statusLine(m)); !strings.HasPrefix(got, "^B … SESSION web / prod-web") {
		t.Errorf("status with the prefix armed = %q", got)
	}
	press(m, keyTab)
	if got := strings.TrimSpace(statusLine(m)); !strings.HasPrefix(got, "LIST web / prod-web") {
		t.Errorf("status = %q, want the LIST tag after ^B Tab", got)
	}
	if !strings.Contains(m.renderStatus(m.w), styled(accentStyle, "LIST")) {
		t.Error("the LIST tag is not in the accent colour")
	}
	if !strings.Contains(m.renderStatus(m.w), styled(accentStyle, "LIST")+"\x1b[m"+styled(barStyle, " web")) {
		t.Error("the status bar's reverse video does not carry on after the tag")
	}
}

func TestStatusTagShowsInScrollMode(t *testing.T) {
	m, _ := newPaneModel(t, "hello")
	press(m, keyPrefix, text("s")[0])
	if got := strings.TrimSpace(statusLine(m)); !strings.HasPrefix(got, "SESSION 0/") {
		t.Errorf("status = %q, want the SESSION tag in scroll mode", got)
	}
}

func TestStatusTagHiddenInFilterAndModal(t *testing.T) {
	m := newTestModel(t, 100, 10)
	press(m, text("/we")...)
	if got := strings.TrimSpace(statusLine(m)); strings.HasPrefix(got, "LIST") {
		t.Errorf("status while filtering = %q, want no tag", got)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	press(m, text("d")...) // delete web's header asks
	if got := strings.TrimSpace(statusLine(m)); strings.HasPrefix(got, "LIST") {
		t.Errorf("status in a modal = %q, want no tag", got)
	}
}

func TestFocusToTheEmptyPaneSaysWhy(t *testing.T) {
	m := newTestModel(t, 100, 10)
	press(m, keyPrefix, keyTab)
	if m.focus != focusList {
		t.Error("^B Tab with no open Session moved focus off the list")
	}
	if m.status != "no open session — press Enter on a connection" {
		t.Errorf("status = %q", m.status)
	}
}

func TestCurrentTabShowsPaneFocus(t *testing.T) {
	m := newTestModel(t, 100, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	focused := styled(accentStyle, "[1 ssh ●]")
	if !strings.Contains(m.render(), focused) {
		t.Error("the current tab is not in the accent colour while the pane has focus")
	}
	press(m, keyPrefix, keyTab)
	if strings.Contains(m.render(), focused) {
		t.Error("the current tab is in the accent colour while the list has focus")
	}
	if !strings.Contains(m.render(), styled(activeRow, "[1 ssh ●]")) {
		t.Error("the current tab is not in reverse video while the list has focus")
	}
}

func TestListCursorShowsListFocus(t *testing.T) {
	m := newTestModel(t, 100, 10)
	m.st.SetCursor("db-main")
	if !strings.Contains(m.render(), styled(focusRowStyle, "  ○ db-main postgres")) {
		t.Error("the cursor row is not cyan reverse while the list has focus")
	}
	press(m, keyEnter)
	if strings.Contains(m.render(), styled(focusRowStyle, "  ● db-main")) {
		t.Error("the cursor row is cyan reverse while the pane has focus")
	}
	if !strings.Contains(m.render(), styled(dimStyle, "›")) {
		t.Error("the › cursor is not dim while the pane has focus")
	}
}
