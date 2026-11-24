package workspace

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newKeyModel is a w by h Workspace whose catalog lists two keys, a
// certificate of the second, and the Spaces default (1, active) and acme
// (2), with id_ed25519 the global Selected identity.
func newKeyModel(t *testing.T, w, h int) (*Model, *fakeCatalog) {
	t.Helper()
	f := &fakeCatalog{
		entries:  []Entry{{Alias: "staging", Group: "web"}, {Alias: "db", Space: "acme"}},
		groups:   []string{"web"},
		spaces:   []Space{{Name: "acme"}},
		identity: "/home/me/.ssh/id_ed25519",
		keys:     []string{"/home/me/.ssh/id_ed25519.pub", "/home/me/.ssh/id_work-cert.pub", "/home/me/.ssh/id_work.pub"},
	}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, f
}

func TestKeyPickerSetsTheActiveSpacesSelectedIdentity(t *testing.T) {
	m, f := newKeyModel(t, 80, 12)
	press(m, text("2")...) // acme
	press(m, text("I")...)
	press(m, keyDown, keyDown) // OpenSSH default, id_ed25519, id_work
	press(m, keyEnter)
	if want := []string{"space acme /home/me/.ssh/id_work"}; !reflect.DeepEqual(f.identityOps, want) {
		t.Fatalf("identity calls = %q, want %q", f.identityOps, want)
	}
	if s := screen(m); !strings.Contains(s, "new tabs in space acme use id_work; open tabs keep their key") {
		t.Errorf("status line does not say what changed:\n%s", s)
	}
	if s := m.sessionStatus(); !strings.Contains(s, "key: id_work") {
		t.Errorf("status line = %q, want acme's new key", s)
	}
	press(m, text("1")...) // the default Space keeps the global one
	if s := m.sessionStatus(); !strings.Contains(s, "key: id_ed25519") {
		t.Errorf("status line = %q, want the global key in the default Space", s)
	}
}

func TestKeyPickerSetsTheGlobalSelectedIdentity(t *testing.T) {
	m, f := newKeyModel(t, 80, 12)
	press(m, keyPrefix, text("I")[0])
	press(m, keyDown) // from id_ed25519, the key in use, to id_work
	press(m, text("g")...)
	press(m, text("I")...) // reopened on id_work, now in use
	press(m, keyUp, keyUp) // OpenSSH default
	press(m, text("g")...)
	want := []string{"global /home/me/.ssh/id_work", "global "}
	if !reflect.DeepEqual(f.identityOps, want) {
		t.Fatalf("identity calls = %q, want %q", f.identityOps, want)
	}
	if s := m.sessionStatus(); !strings.Contains(s, "key: OpenSSH default") {
		t.Errorf("status line = %q, want OpenSSH default", s)
	}
}

func TestKeyPickerOpenSSHDefaultUnsetsTheSpacesKey(t *testing.T) {
	m, f := newKeyModel(t, 80, 12)
	f.spaceKeys = map[string]string{"acme": "/home/me/.ssh/id_work"}
	press(m, keyPrefix, text("R")[0])
	press(m, text("2")...)
	press(m, text("I")...)
	press(m, keyUp, keyUp, keyUp) // to the top: OpenSSH default
	press(m, keyEnter)
	if want := []string{"space acme "}; !reflect.DeepEqual(f.identityOps, want) {
		t.Fatalf("identity calls = %q, want %q", f.identityOps, want)
	}
	if s := m.sessionStatus(); !strings.Contains(s, "key: id_ed25519") {
		t.Errorf("status line = %q, want acme back on the global key", s)
	}
}

func TestKeyPickerClosesWithoutChanges(t *testing.T) {
	for _, k := range []string{"esc", "q", "I"} {
		m, f := newKeyModel(t, 80, 12)
		press(m, text("I")...)
		msg := keyEsc
		if k != "esc" {
			msg = text(k)[0]
		}
		press(m, msg)
		if m.mode.modal != nil || len(f.identityOps) > 0 {
			t.Errorf("%s: modal %T, identity calls %q; want closed, none", k, m.mode.modal, f.identityOps)
		}
	}
}

func TestKeyPickerGolden(t *testing.T) {
	m, f := newKeyModel(t, 80, 12)
	f.spaceKeys = map[string]string{"acme": "/home/me/.ssh/id_work"}
	press(m, keyPrefix, text("R")[0])
	press(m, text("2")...)
	press(m, text("I")...)
	golden(t, "key-picker", m)
}
