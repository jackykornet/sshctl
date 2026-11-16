package workspace

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var bootstrapLaunch = Launch{Action: ActionBootstrap}

func TestPrefixIBootstrapsCursorConnectionInItsOwnTab(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}, {Alias: "beta"}})
	press(m, keyDown)
	press(m, keyPrefix, text("i")[0])
	want := commandCall{"beta", bootstrapLaunch}
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", r.calls, want)
	}
	c := m.st.Active()
	if c == nil || c.Alias != "beta" || len(c.Sessions) != 1 || c.Sessions[0].Action != ActionBootstrap {
		t.Fatalf("active = %+v, want beta with one Bootstrap tab", c)
	}
}

func TestSuccessfulBootstrapReloadsConnectionList(t *testing.T) {
	for _, tt := range []struct {
		name   string
		code   int
		reload bool
	}{
		{"success reloads", 0, true},
		{"failure leaves the list", 1, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, r := newRecordingModel(t, []Entry{{Alias: "alpha", Managed: true}})
			press(m, keyPrefix, text("i")[0])
			// Bootstrap rewrote alpha's Host block while its tab ran.
			r.entries = []Entry{{Alias: "alpha", Managed: true, Description: "bootstrapped"}}
			endSession(m, m.st.Active().CurrentSession(), tt.code)
			if got := m.st.Get("alpha").Description == "bootstrapped"; got != tt.reload {
				t.Fatalf("list reloaded = %v, want %v", got, tt.reload)
			}
			if !m.st.Get("alpha").Open() || !m.st.Active().CurrentSession().Ended {
				t.Fatal("the ended Bootstrap tab stays open")
			}
		})
	}
}

func TestEndedShellSessionDoesNotReload(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	press(m, keyEnter)
	loads := r.loads
	endSession(m, m.st.Active().CurrentSession(), 0)
	if r.loads != loads {
		t.Fatalf("Snapshot calls = %d, want %d: only an Action reloads", r.loads, loads)
	}
}

func TestBootstrapTabAnswersPromptsAndShowsExitCode(t *testing.T) {
	m, _ := newSessionModel(t, script{
		sh:  `printf '%s password: ' "$1"; read -r pw; echo "key installed with $pw"; exit 2`,
		cmd: `set /p pw={alias} password: & echo key installed with !pw!& exit 2`,
	})
	press(m, keyPrefix, text("i")[0])
	sess := m.st.Active().CurrentSession()
	if sess.Action != ActionBootstrap {
		t.Fatalf("session = %+v, want a Bootstrap tab", sess)
	}
	pump(t, m, func() bool { return strings.Contains(screen(m), "alpha password:") })
	press(m, text("hunter2")...)
	press(m, keyEnter)
	pump(t, m, func() bool { return sess.Ended })
	got := screen(m)
	if !strings.Contains(got, "key installed with hunter2") || !strings.Contains(got, "session ended (exit 2)") || !strings.Contains(got, "alpha 1 bootstrap") {
		t.Fatalf("exit=%d screen:\n%s", sess.ExitCode, got)
	}
}

func TestPrefixIBootstrapsActiveConnectionWhilePaneHasFocus(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}, {Alias: "beta"}})
	press(m, keyEnter) // alpha 1: ssh, focus moves to the pane
	m.st.MoveCursor(1) // the cursor moving to beta does not change the target
	press(m, keyPrefix, text("i")[0])
	want := []commandCall{{"alpha", Launch{}}, {"alpha", bootstrapLaunch}}
	if !slices.Equal(r.calls, want) {
		t.Fatalf("Command calls = %v, want %v", r.calls, want)
	}
	bar, _ := m.tabBar(60)
	if got := ansi.Strip(bar); !strings.Contains(got, " alpha 1  alpha 2 bootstrap ") {
		t.Fatalf("tab bar = %q, want a labelled Bootstrap tab", got)
	}
}

var keyCtrlO = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

func TestSaveAndBootstrapOpensBootstrapTabForSavedAlias(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	press(m, keyCtrlO)
	if m.form() != nil || !slices.Equal(r.saves, []string{"→api"}) {
		t.Fatalf("form = %+v saves = %v", m.form(), r.saves)
	}
	want := commandCall{"api", bootstrapLaunch}
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", r.calls, want)
	}
	if c := m.st.Active(); c == nil || c.Alias != "api" || c.CurrentSession().Action != ActionBootstrap {
		t.Fatalf("active = %+v, want api's Bootstrap tab", c)
	}
}

func TestSaveAndBootstrapWaitsForTheInclude(t *testing.T) {
	m, r := newRecordingModel(t, nil)
	r.saveErrs = []error{needInclude}
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	press(m, keyCtrlO)
	if len(r.calls) != 0 {
		t.Fatalf("Bootstrap started before the Include was added: %v", r.calls)
	}
	press(m, text("y")...)
	want := commandCall{"api", bootstrapLaunch}
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", r.calls, want)
	}
}

func TestFailedSaveStartsNoBootstrap(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		decline bool
	}{
		{"save error", errors.New("hostname is required"), false},
		{"Include declined", needInclude, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, r := newRecordingModel(t, nil)
			r.saveErrs = []error{tt.err}
			press(m, text("a")...)
			fill(m, "api")
			press(m, keyCtrlO)
			if tt.decline {
				press(m, text("n")...)
			}
			if len(r.calls) != 0 {
				t.Fatalf("Command calls = %v, want none", r.calls)
			}
			if m.form() == nil {
				t.Fatal("the form should stay open")
			}
			if !tt.decline && m.form().err != tt.err.Error() {
				t.Fatalf("form error = %q", m.form().err)
			}
		})
	}
}

func TestPlainSaveStartsNoBootstrap(t *testing.T) {
	m, r := newRecordingModel(t, nil)
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	press(m, keyCtrlS)
	if m.form() != nil || len(r.saves) != 1 || len(r.calls) != 0 {
		t.Fatalf("form = %+v saves = %v calls = %v", m.form(), r.saves, r.calls)
	}
}

func TestEditFormHasNoSaveAndBootstrap(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha", Managed: true, Host: Host{HostName: "h"}}})
	press(m, text("e")...)
	press(m, keyCtrlO)
	if m.form() == nil || len(r.saves) != 0 || len(r.calls) != 0 {
		t.Fatalf("form = %+v saves = %v calls = %v", m.form(), r.saves, r.calls)
	}
}
