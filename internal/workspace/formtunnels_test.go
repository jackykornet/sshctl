package workspace

import (
	"reflect"
	"strings"
	"testing"
)

var (
	panelTunnel   = Tunnel{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}
	cockpitTunnel = Tunnel{Name: "cockpit", LocalPort: 9090, RemoteHost: "localhost", RemotePort: 9090}
)

// focusFormField puts the open form's focus on its field id.
func focusFormField(t *testing.T, m *Model, id fieldID) {
	t.Helper()
	f := m.form()
	for i, fl := range f.fields {
		if fl.id == id {
			f.focusField(i)
			return
		}
	}
	t.Fatalf("the form has no field %d", id)
}

// typeTunnels replaces the open form's Tunnels field with text.
func typeTunnels(t *testing.T, m *Model, text_ string) {
	t.Helper()
	focusFormField(t, m, fieldTunnels)
	press(m, keyClear)
	press(m, text(text_)...)
}

func TestTheEditFormRoundTripsTunnels(t *testing.T) {
	entries := storeEntries()
	want := []Tunnel{panelTunnel, {LocalPort: 5433, RemoteHost: "10.0.0.5", RemotePort: 5432}}
	entries[0].Tunnels = want
	f := &fakeCatalog{entries: entries}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("e")...)
	if got := formValues(m)["Tunnels"]; got != "panel=8083, 5433:10.0.0.5:5432" {
		t.Fatalf("Tunnels field = %q", got)
	}
	press(m, keyCtrlS)
	if len(f.specs) != 1 || !reflect.DeepEqual(f.specs[0].Tunnels, want) {
		t.Errorf("saved %+v, want the Tunnels unchanged", f.specs)
	}
}

func TestTheFormSavesTypedTunnels(t *testing.T) {
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	typeTunnels(t, m, "panel=8083, cockpit=9090")
	press(m, keyCtrlS)
	if m.form() != nil {
		t.Fatalf("form still open: %s", m.form().err)
	}
	if len(f.specs) != 1 || !reflect.DeepEqual(f.specs[0].Tunnels, []Tunnel{panelTunnel, cockpitTunnel}) {
		t.Errorf("saved %+v, want panel and cockpit", f.specs)
	}
}

// Tunnels are sshctl metadata, so a Connection whose Host block is the
// user's has the field too, and emptying it saves none.
func TestTheFormEditsTunnelsOfAUserOwnedConnection(t *testing.T) {
	entries := storeEntries()
	entries[1].Tunnels = []Tunnel{panelTunnel}
	f := &fakeCatalog{entries: entries}
	m := newStoreModel(t, f)
	m.st.SetCursor("mine")
	press(m, text("e")...)
	typeTunnels(t, m, "")
	press(m, keyCtrlS)
	if len(f.specs) != 1 || f.specs[0].Tunnels != nil || f.specs[0].Managed {
		t.Errorf("saved %+v, want mine's metadata with no Tunnels", f.specs)
	}
}

func TestTheFormRefusesABadTunnelAndSavesNothing(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"panel=8083, 70000", "1 to 65535"},
		{"8083, panel=8083", "local port 8083"},
		{"42=8083", "only digits"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			f := &fakeCatalog{entries: storeEntries()}
			m := newStoreModel(t, f)
			m.st.SetCursor("web")
			press(m, text("e")...)
			typeTunnels(t, m, tc.text)
			focusFormField(t, m, fieldDescription)
			press(m, keyCtrlS)
			form := m.form()
			if form == nil || len(f.specs) != 0 {
				t.Fatalf("saved %+v, want the form left open", f.specs)
			}
			if !strings.Contains(form.err, tc.want) {
				t.Errorf("error %q, want it to mention %q", form.err, tc.want)
			}
			if form.fields[form.focus].id != fieldTunnels {
				t.Errorf("focus on %q, want the Tunnels field", form.fields[form.focus].label)
			}
		})
	}
}

func TestTheEditFormCanOpenOnTheTunnelsField(t *testing.T) {
	for _, alias := range []string{"web", "mine"} {
		m := newStoreModel(t, &fakeCatalog{entries: storeEntries()})
		m.st.SetCursor(alias)
		m.startEditAt(fieldTunnels)
		f := m.form()
		if f == nil || f.fields[f.focus].id != fieldTunnels {
			t.Fatalf("%s: form %+v, want it open on the Tunnels field", alias, f)
		}
	}
}
