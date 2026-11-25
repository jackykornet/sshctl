package workspace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSessionLabel(t *testing.T) {
	tests := []struct {
		name string
		sess Session
		want string
	}{
		{"ssh", Session{}, "ssh"},
		{"typed name", Session{Name: "logs"}, "logs"},
		{"action", Session{Launch: Launch{Action: "provision"}}, "provision"},
		{"typed name wins over the action", Session{Name: "dotfiles", Launch: Launch{Action: "provision"}}, "dotfiles"},
	}
	for _, tt := range tests {
		if got := tt.sess.Label(); got != tt.want {
			t.Errorf("%s: Label() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTabLabel(t *testing.T) {
	tests := []struct {
		name string
		sess Session
		want string
	}{
		{"ssh", Session{N: 1}, " 1 ssh "},
		{"name", Session{N: 2, Name: "logs"}, " 2 logs "},
		{"unseen", Session{N: 3, Unseen: true}, " 3 ssh • "},
		{"ended shows the exit code, not unseen", Session{N: 4, Ended: true, ExitCode: 255, Unseen: true}, " 4 ssh ✕ 255 "},
		{"ended with 0", Session{N: 5, Ended: true}, " 5 ssh ✕ 0 "},
		{"long name is cut", Session{N: 6, Name: strings.Repeat("x", 30)}, " 6 " + strings.Repeat("x", 19) + "… "},
	}
	for _, tt := range tests {
		label, plain := tabLabel(&tt.sess, false, false, "")
		if plain != tt.want {
			t.Errorf("%s: plain = %q, want %q", tt.name, plain, tt.want)
		}
		if got := ansi.Strip(label); got != plain {
			t.Errorf("%s: label = %q, want it to read %q", tt.name, got, plain)
		}
	}
}

func TestRemoteTitleDoesNotNameTab(t *testing.T) {
	m, sess, _ := paneModel(t, Options{}, "\x1b]2;C:\\Windows\\system32\\cmd.exe\x07")
	m.Update(redrawMsg{})
	if bar, _ := m.tabBar(60); sess.Label() != "ssh" || !strings.Contains(ansi.Strip(bar), " 1 ssh │ + ") {
		t.Errorf("Label = %q, tab bar = %q; want ssh", sess.Label(), ansi.Strip(bar))
	}
}

func TestRenameTab(t *testing.T) {
	m, sess, _ := paneModel(t, Options{}, "")
	comma := text(",")[0]
	press(m, keyPrefix, comma)
	if got, _ := m.mode.modal.status(); !strings.HasPrefix(got, "rename tab 1 to: ") {
		t.Fatalf("status = %q", got)
	}
	press(m, append(text("logs"), keyEnter)...)
	if sess.Name != "logs" || m.mode.modal != nil {
		t.Fatalf("Name = %q, modal = %v", sess.Name, m.mode.modal)
	}
	if !strings.Contains(screen(m), " 1 logs │ + ") {
		t.Errorf("screen lacks the named tab:\n%s", screen(m))
	}

	// Empty hands the tab back to its automatic name.
	// The prompt starts with the typed name; empty gives back ssh.
	press(m, keyPrefix, comma)
	if got, _ := m.mode.modal.status(); !strings.HasPrefix(got, "rename tab 1 to: logs") {
		t.Fatalf("status = %q, want the typed name", got)
	}
	press(m, keyBackspace, keyBackspace, keyBackspace, keyBackspace, keyEnter)
	if sess.Name != "" || sess.Label() != "ssh" {
		t.Errorf("Name = %q, Label = %q after clearing", sess.Name, sess.Label())
	}

	// Esc leaves the name alone.
	press(m, keyPrefix, comma)
	press(m, append(text("x"), keyEsc)...)
	if sess.Name != "" {
		t.Errorf("Name = %q after Esc", sess.Name)
	}
}

func TestReconnectKeepsTypedName(t *testing.T) {
	m, _ := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	sess.Ended, sess.Name = true, "logs"
	if err := m.sessions.reconnect(sess); err != nil {
		t.Fatal(err)
	}
	if sess.Name != "logs" {
		t.Errorf("after reconnect Name = %q, want logs", sess.Name)
	}
}

func TestTabNamesGolden(t *testing.T) {
	m := newTestModel(t, 100, 8)
	m.st.SetCursor("db-main")
	press(m, keyEnter) // db-main 1
	press(m, keyPrefix, text("n")[0], keyPrefix, text("n")[0], keyPrefix, text("n")[0])
	db := m.st.Get("db-main")
	db.Sessions[1].Name = "logs"
	m.st.MarkActivity(db.Sessions[1])
	db.Sessions[2].Action = "provision"
	endSession(m, db.Sessions[2], 255)
	db.Sessions[3].Name = "vim notes.txt"
	golden(t, "tab-names", m)
}
