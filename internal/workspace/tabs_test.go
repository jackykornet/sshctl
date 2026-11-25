package workspace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCleanTitle(t *testing.T) {
	tests := []struct{ title, want string }{
		{"jacky@web: ~/src", "~/src"},
		{"jacky@web:~/src", "~/src"},
		{"root@db-main", ""},
		{"root@db-main vim notes.txt", "vim notes.txt"},
		{"vim notes.txt", "vim notes.txt"},
		{"htop", "htop"},
		{"  spaced  ", "spaced"},
		{"a\x1b[31mred\x07", "a[31mred"},
		{"mail me@example.com", "mail me@example.com"},
	}
	for _, tt := range tests {
		if got := cleanTitle(tt.title); got != tt.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestSessionLabel(t *testing.T) {
	tests := []struct {
		name string
		sess Session
		want string
	}{
		{"nothing", Session{}, ""},
		{"remote title", Session{Title: "~/src"}, "~/src"},
		{"typed name wins over the title", Session{Name: "logs", Title: "~/src"}, "logs"},
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
		{"number only", Session{N: 1}, " 1 "},
		{"name", Session{N: 2, Title: "~/src"}, " 2 ~/src "},
		{"unseen", Session{N: 3, Unseen: true}, " 3 • "},
		{"ended shows the exit code, not unseen", Session{N: 4, Ended: true, ExitCode: 255, Unseen: true}, " 4 ✕ 255 "},
		{"ended with 0", Session{N: 5, Ended: true}, " 5 ✕ 0 "},
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

// titledModel is a Workspace with alpha open and its Session's emulator
// reporting window titles as a real runner's does.
func titledModel(t *testing.T) (*Model, *Session, *runner) {
	t.Helper()
	m, sess, _ := paneModel(t, Options{}, "")
	r := m.sessions.runners[sess]
	w, h := m.paneSize()
	r.term = newVterm(w, h, 100, nil, func(title string) { r.title.Store(&title) })
	return m, sess, r
}

func TestRemoteTitleNamesTab(t *testing.T) {
	m, sess, r := titledModel(t)
	r.term.Write([]byte("\x1b]2;jacky@alpha: ~/src\x07"))
	r.activity.Store(true)
	m.Update(redrawMsg{})
	if sess.Title != "~/src" {
		t.Fatalf("Title = %q, want ~/src", sess.Title)
	}
	if bar, _ := m.tabBar(60); !strings.Contains(ansi.Strip(bar), " 1 ~/src │ + ") {
		t.Errorf("tab bar = %q", ansi.Strip(bar))
	}
}

func TestActionTabIgnoresRemoteTitle(t *testing.T) {
	m, sess, r := titledModel(t)
	sess.Action = "provision"
	r.term.Write([]byte("\x1b]0;script\x07"))
	m.Update(redrawMsg{})
	if sess.Title != "" || sess.Label() != "provision" {
		t.Errorf("Title = %q, Label = %q; want the action's name", sess.Title, sess.Label())
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
	sess.Title = "~/src"
	press(m, keyPrefix, comma)
	press(m, keyBackspace, keyBackspace, keyBackspace, keyBackspace, keyEnter)
	if sess.Name != "" || sess.Label() != "~/src" {
		t.Errorf("Name = %q, Label = %q after clearing", sess.Name, sess.Label())
	}

	// Enter on the automatic name keeps following it.
	press(m, keyPrefix, comma, keyEnter)
	if sess.Name != "" {
		t.Errorf("Name = %q after Enter on the automatic name, want none", sess.Name)
	}

	// Esc leaves the name alone.
	press(m, keyPrefix, comma)
	press(m, append(text("x"), keyEsc)...)
	if sess.Name != "" {
		t.Errorf("Name = %q after Esc", sess.Name)
	}
}

func TestReconnectKeepsTypedNameAndDropsTitle(t *testing.T) {
	m, _ := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	sess.Ended, sess.Name, sess.Title = true, "logs", "~/src"
	if err := m.sessions.reconnect(sess); err != nil {
		t.Fatal(err)
	}
	if sess.Name != "logs" || sess.Title != "" {
		t.Errorf("after reconnect Name = %q, Title = %q; want logs and none", sess.Name, sess.Title)
	}
}

func TestTabNamesGolden(t *testing.T) {
	m := newTestModel(t, 100, 8)
	m.st.SetCursor("db-main")
	press(m, keyEnter) // db-main 1
	press(m, keyPrefix, text("n")[0], keyPrefix, text("n")[0], keyPrefix, text("n")[0])
	db := m.st.Get("db-main")
	db.Sessions[0].Title = "~/src"
	db.Sessions[1].Name = "logs"
	m.st.MarkActivity(db.Sessions[1])
	db.Sessions[2].Action = "provision"
	endSession(m, db.Sessions[2], 255)
	db.Sessions[3].Title = "vim notes.txt"
	golden(t, "tab-names", m)
}
