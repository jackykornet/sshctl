package workspace

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// needInclude is the error the catalog returns before ssh_config Includes sshctl's file.
var needInclude = &IncludeError{Line: "Include sshctl_config", Config: "~/.ssh/config"}

func newStoreModel(t *testing.T, f *fakeCatalog) *Model {
	t.Helper()
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	return m
}

func storeEntries() []Entry {
	return []Entry{
		{Alias: "web", Group: "prod", Managed: true, Host: Host{HostName: "10.0.0.1", User: "deploy"}},
		{Alias: "mine", Description: "hand written"},
	}
}

var keyCtrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}

// fill types values into the form's fields from the focused one on, moving
// to the next field after each.
func fill(m *Model, values ...string) {
	for _, v := range values {
		press(m, keyClear)
		press(m, text(v)...)
		press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	}
}

func TestAddConnectionAsksForIncludeThenSaves(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries(), saveErrs: []error{needInclude}}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("a")...)
	if m.form() == nil || m.form().old != "" {
		t.Fatal("a should open the add form")
	}
	if got := m.form().spec().Group; got != "prod" {
		t.Errorf("group prefilled with %q, want the cursor's group prod", got)
	}
	fill(m, "api", "10.0.0.9", "", "2222")
	press(m, keyCtrlS)
	if m.confirm() == nil || !strings.Contains(m.confirm().question, "Include sshctl_config") {
		t.Fatalf("want a question about the Include line, got %+v", m.confirm())
	}
	press(m, text("y")...)
	if m.form() != nil || f.includes != 1 {
		t.Fatalf("form = %+v includes = %d", m.form(), f.includes)
	}
	if want := []string{"→api", "→api"}; !reflect.DeepEqual(f.saves, want) {
		t.Fatalf("saves = %v, want %v: the save is retried after the Include", f.saves, want)
	}
	c := m.st.Get("api")
	if c == nil || c.Group != "prod" || c.Host.Port != "2222" || !c.Managed {
		t.Fatalf("api = %+v", c)
	}
	if m.st.Cursor() != c || m.status != "added api" {
		t.Errorf("cursor = %v status = %q", m.st.Cursor(), m.status)
	}
}

// A hosts file synced to a new machine without the Include (ADR-0004) is
// offered the Include as soon as the Workspace starts.
func TestStartupOffersTheMissingInclude(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries(), include: needInclude}
	m := newStoreModel(t, f)
	if m.confirm() == nil || !strings.Contains(m.confirm().question, `"Include sshctl_config" to the top of ~/.ssh/config`) {
		t.Fatalf("want the Include question at startup, got %+v", m.confirm())
	}
	press(m, text("y")...)
	if m.confirm() != nil || f.includes != 1 {
		t.Fatalf("confirm = %+v includes = %d", m.confirm(), f.includes)
	}
	if m.statusErr || !strings.Contains(m.status, "Include sshctl_config") {
		t.Errorf("status = %q (err %v)", m.status, m.statusErr)
	}
}

func TestDecliningTheStartupIncludeLastsTheSession(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries(), include: needInclude}
	m := newStoreModel(t, f)
	press(m, text("n")...)
	if m.confirm() != nil || f.includes != 0 || len(f.saves) != 0 {
		t.Fatalf("confirm = %+v includes = %d saves = %v", m.confirm(), f.includes, f.saves)
	}
	press(m, keyPrefix, text("R")[0])
	if m.confirm() != nil || m.status != "connection list refreshed" {
		t.Fatalf("refresh asked again: confirm = %+v status = %q", m.confirm(), m.status)
	}
}

func TestStartupAsksNothingWhenTheIncludeIsFine(t *testing.T) {
	m := newStoreModel(t, &fakeCatalog{entries: storeEntries()})
	if m.confirm() != nil {
		t.Fatalf("unexpected question %q", m.confirm().question)
	}
}

func TestDecliningIncludeKeepsTheForm(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries(), saveErrs: []error{needInclude}}
	m := newStoreModel(t, f)
	m.st.SetCursor("mine") // on a header, a adds a Group
	press(m, keyPrefix, text("a")[0])
	fill(m, "api", "h")
	press(m, keyCtrlS, text("n")[0])
	if m.form() == nil || len(f.saves) != 1 || f.includes != 0 {
		t.Fatalf("form = %v saves = %v includes = %d", m.form(), f.saves, f.includes)
	}
}

func TestSaveErrorStaysInForm(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries(), saveErrs: []error{errors.New("hostname is required")}}
	m := newStoreModel(t, f)
	m.st.SetCursor("mine")
	press(m, text("a")...)
	press(m, keyCtrlS)
	if m.form() == nil || m.form().err != "hostname is required" {
		t.Fatalf("form = %+v", m.form())
	}
	if !strings.Contains(ansi.Strip(m.render()), "hostname is required") {
		t.Error("the error should be shown in the form")
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.form() != nil {
		t.Fatal("Esc should close the form")
	}
}

func TestEditManagedConnectionCanRename(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("e")...)
	if got := m.form().spec(); got.Alias != "web" || got.Host.User != "deploy" || len(m.form().fields) != 8 {
		t.Fatalf("form = %+v", got)
	}
	fill(m, "web1")
	press(m, keyCtrlS)
	if want := []string{"web→web1"}; !reflect.DeepEqual(f.saves, want) {
		t.Fatalf("saves = %v, want %v", f.saves, want)
	}
	if m.st.Get("web") != nil || m.st.Get("web1") == nil {
		t.Fatal("the list should show the renamed connection")
	}
}

func TestRenamingOpenConnectionIsRefused(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.OpenSession("web") // active, terminal focus not needed: the cursor is on it
	press(m, keyPrefix, text("e")[0])
	fill(m, "web1")
	press(m, keyCtrlS)
	if m.form() == nil || !strings.Contains(m.form().err, "close web's tabs") || len(f.saves) != 0 {
		t.Fatalf("form = %+v saves = %v", m.form(), f.saves)
	}
}

func TestEditUnmanagedConnectionOnlyChangesMetadata(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.SetCursor("mine")
	press(m, text("e")...)
	if len(m.form().fields) != 3 {
		t.Fatalf("fields = %d, want description, group and tunnels only", len(m.form().fields))
	}
	fill(m, "laptop", "home")
	press(m, keyCtrlS)
	want := Spec{Alias: "mine", Description: "laptop", Group: "home"}
	if !reflect.DeepEqual(f.specs, []Spec{want}) {
		t.Fatalf("saved %+v, want only %+v", f.specs, want)
	}
	if c := m.st.Get("mine"); c.Description != "laptop" || c.Group != "home" || c.Managed {
		t.Fatalf("mine = %+v", c)
	}
}

func TestDeleteConnectionAsksFirst(t *testing.T) {
	tests := []struct {
		alias    string
		question string
	}{
		{"web", "Delete web from ssh_config?"},
		{"mine", "Remove mine's description, group and tunnels?"},
	}
	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			f := &fakeCatalog{entries: storeEntries()}
			m := newStoreModel(t, f)
			m.st.SetCursor(tt.alias)
			press(m, text("d")...)
			if m.confirm() == nil || !strings.HasPrefix(m.confirm().question, tt.question) {
				t.Fatalf("confirm = %+v, want %q", m.confirm(), tt.question)
			}
			press(m, text("y")...)
			if !reflect.DeepEqual(f.deletes, []string{tt.alias}) || m.st.Get(tt.alias) != nil {
				t.Fatalf("deletes = %v, still listed = %v", f.deletes, m.st.Get(tt.alias) != nil)
			}
		})
	}
}

func TestDeleteRefusesOpenOrForeignConnections(t *testing.T) {
	f := &fakeCatalog{entries: append(storeEntries(), Entry{Alias: "bare"})}
	m := newStoreModel(t, f)
	m.st.OpenSession("web")
	press(m, keyPrefix, text("d")[0]) // web is active and open
	m.st.SetCursor("bare")
	m.focus = focusList
	press(m, text("d")...)
	if m.confirm() != nil || len(f.deletes) != 0 {
		t.Fatalf("confirm = %+v deletes = %v", m.confirm(), f.deletes)
	}
	if !strings.Contains(m.status, "defined outside sshctl") {
		t.Errorf("status = %q", m.status)
	}
}

func TestFormIsDrawnInThePane(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.SetCursor("mine")
	press(m, text("a")...)
	screen := ansi.Strip(m.render())
	for _, want := range []string{"Add connection", "› Alias", "HostName", "host or IP address", "^S save"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen missing %q:\n%s", want, screen)
		}
	}
	m.Update(tea.MouseClickMsg{X: 3, Y: 1, Button: tea.MouseLeft})
	if m.form() == nil {
		t.Error("the mouse must not act behind an open form")
	}
}

func TestPrefixCommandsDoNothingBehindTheForm(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	loads := f.loads
	m.st.SetCursor("web")
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	for _, k := range "nar" {
		press(m, keyPrefix, tea.KeyPressMsg{Code: k, Text: string(k)})
	}
	if n := len(m.st.Get("web").Sessions); n != 0 {
		t.Errorf("^B n opened %d Sessions behind the form", n)
	}
	if n := f.loads - loads; n != 0 {
		t.Errorf("^B r reloaded %d times behind the form", n)
	}
	if m.form() == nil {
		t.Fatal("the form should still be open")
	}
	if s := m.form().spec(); s.Alias != "api" || s.Host.HostName != "10.0.0.9" {
		t.Fatalf("form lost its contents: %+v", s)
	}
	press(m, keyCtrlS)
	if m.form() != nil || !reflect.DeepEqual(f.saves, []string{"→api"}) {
		t.Fatalf("saving after prefix keys: form = %+v saves = %v", m.form(), f.saves)
	}
	press(m, text("a")...)
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyEscape})
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.form() != nil {
		t.Fatal("Esc should still close the form")
	}
}

func TestEditKeepsTheConnectionInItsSpace(t *testing.T) {
	entries := storeEntries()
	for i := range entries {
		if entries[i].Alias == "mine" {
			entries[i].Space = "acme"
		}
	}
	f := &fakeCatalog{entries: entries, spaces: []Space{{Name: "acme"}}, activeSpace: "acme"}
	m := newStoreModel(t, f)
	m.st.SetCursor("mine")
	press(m, text("e")...)
	fill(m, "laptop")
	press(m, keyCtrlS)
	if len(f.specs) != 1 || f.specs[0].Space != "acme" {
		t.Fatalf("saved %+v, want mine kept in acme", f.specs)
	}
}
