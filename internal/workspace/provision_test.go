package workspace

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/sshctl/sshctl/internal/catalog"
)

var provisionLaunch = Launch{Action: ActionProvision}

func TestPrefixPProvisionsCursorConnectionAfterConfirming(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}, {Alias: "beta"}})
	press(m, keyDown)
	press(m, keyPrefix, text("p")[0])
	if c := m.confirm(); c == nil || c.question != "Provision beta with your dotfiles?" {
		t.Fatalf("confirm = %+v, want the Provision question for beta", c)
	}
	if len(r.calls) != 0 {
		t.Fatalf("Command calls = %v before the answer", r.calls)
	}
	press(m, text("y")...)
	want := commandCall{"beta", provisionLaunch}
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", r.calls, want)
	}
	c := m.st.Active()
	if c == nil || c.Alias != "beta" || len(c.Sessions) != 1 || c.Sessions[0].Action != ActionProvision {
		t.Fatalf("active = %+v, want beta with one Provision tab", c)
	}
}

func TestProvisionRefusedByGroupOpensNoTab(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha", Group: "prod"}})
	r.groups = []string{"prod"}
	r.provision = map[string]error{
		"alpha": fmt.Errorf("%w of group prod", catalog.ErrProvisionRefused),
	}
	m.reload("", "")
	m.st.SetCursor("alpha")
	press(m, keyPrefix, text("p")[0])
	if c := m.confirm(); c != nil {
		t.Fatalf("asked %q, want a refusal", c.question)
	}
	if len(r.calls) != 0 || m.st.Active() != nil {
		t.Fatalf("Command calls = %v active = %+v, want no tab", r.calls, m.st.Active())
	}
	if want := "alpha: Provision is refused by the dotfiles setting none of group prod"; m.status != want {
		t.Fatalf("status = %q, want %q", m.status, want)
	}
}

func TestProvisionWithoutDotfilesRepoStillOpensTab(t *testing.T) {
	// The first-time repo prompt runs inside the tab.
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	r.provision = map[string]error{"alpha": catalog.ErrNoDotfiles}
	press(m, keyPrefix, text("p")[0])
	press(m, text("y")...)
	if want := []commandCall{{"alpha", provisionLaunch}}; !slices.Equal(r.calls, want) {
		t.Fatalf("Command calls = %v, want %v", r.calls, want)
	}
}

func TestProvisionAnsweredNoOpensNoTab(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	press(m, keyPrefix, text("p")[0])
	press(m, text("n")...)
	if len(r.calls) != 0 || m.confirm() != nil {
		t.Fatalf("Command calls = %v confirm = %+v", r.calls, m.confirm())
	}
}

func TestProvisionTabAsksForRepoAndShowsExitCode(t *testing.T) {
	m, _ := newSessionModel(t, script{
		sh:  `printf 'dotfiles repo for %s: ' "$1"; read -r url; echo "applying $url"; echo "mise install failed"; exit 3`,
		cmd: `set /p url=dotfiles repo for {alias}: & echo applying !url!& echo mise install failed& exit 3`,
	})
	press(m, keyPrefix, text("p")[0])
	press(m, text("y")...)
	sess := m.st.Active().CurrentSession()
	if sess.Action != ActionProvision {
		t.Fatalf("session = %+v, want a Provision tab", sess)
	}
	pump(t, m, func() bool { return strings.Contains(screen(m), "dotfiles repo for alpha:") })
	press(m, text("https://example.com/d.git")...)
	press(m, keyEnter)
	pump(t, m, func() bool { return sess.Ended })
	got := screen(m)
	for _, want := range []string{"applying https://example.com/d.git", "mise install failed", "session ended (exit 3)", " 1 provision ✕ 3 "} {
		if !strings.Contains(got, want) {
			t.Fatalf("screen lacks %q; exit=%d screen:\n%s", want, sess.ExitCode, got)
		}
	}
}
