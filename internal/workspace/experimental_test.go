package workspace

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sshctl/sshctl/internal/catalog"
)

const (
	bootstrapOff = "Bootstrap is experimental and off. Turn it on with: sshctl experimental enable bootstrap"
	provisionOff = "Provision is experimental and off. Turn it on with: sshctl experimental enable provision"
)

// offModel is a recording Workspace whose catalog reports off as off.
func offModel(t *testing.T, entries []Entry, off ...string) (*Model, *recorder) {
	t.Helper()
	m, r := newRecordingModel(t, entries)
	r.off = off
	m.reload("", "")
	return m, r
}

func TestPrefixIRefusesWhileBootstrapIsOff(t *testing.T) {
	m, r := offModel(t, []Entry{{Alias: "alpha"}}, catalog.FeatureBootstrap)
	press(m, keyPrefix, text("i")[0])
	if len(r.calls) != 0 || m.st.Active() != nil {
		t.Fatalf("Command calls = %v active = %+v, want no tab", r.calls, m.st.Active())
	}
	if m.status != bootstrapOff || !m.statusErr {
		t.Errorf("status = %q (err %v), want %q", m.status, m.statusErr, bootstrapOff)
	}
}

func TestPrefixPRefusesWhileProvisionIsOff(t *testing.T) {
	m, r := offModel(t, []Entry{{Alias: "alpha"}}, catalog.FeatureProvision)
	press(m, keyPrefix, text("p")[0])
	if c := m.confirm(); c != nil {
		t.Fatalf("asked %q, want a refusal", c.question)
	}
	if len(r.calls) != 0 || m.st.Active() != nil {
		t.Fatalf("Command calls = %v active = %+v, want no tab", r.calls, m.st.Active())
	}
	if m.status != provisionOff || !m.statusErr {
		t.Errorf("status = %q (err %v), want %q", m.status, m.statusErr, provisionOff)
	}
}

func TestSaveAndBootstrapKeepsTheFormOpenWhileBootstrapIsOff(t *testing.T) {
	m, r := offModel(t, []Entry{{Alias: "alpha"}}, catalog.FeatureBootstrap)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	press(m, text("a")...)
	if !strings.Contains(screen(m), "Ctrl+O save and Bootstrap (experimental, off)") {
		t.Fatalf("form does not mark Ctrl+O off:\n%s", screen(m))
	}
	fill(m, "api", "10.0.0.9")
	press(m, keyCtrlO)
	if m.form() == nil || len(r.saves) != 0 || len(r.calls) != 0 {
		t.Fatalf("form = %v saves = %v calls = %v, want the form open and nothing saved", m.form() != nil, r.saves, r.calls)
	}
	if m.form().err != bootstrapOff {
		t.Errorf("form error = %q, want %q", m.form().err, bootstrapOff)
	}
	press(m, keyCtrlS)
	if !slices.Equal(r.saves, []string{"→api"}) || len(r.calls) != 0 {
		t.Errorf("Ctrl+S saves = %v calls = %v, want api saved without Bootstrap", r.saves, r.calls)
	}
}

func TestHelpScreenSaysWhichExperimentalFeaturesAreOff(t *testing.T) {
	for _, tt := range []struct {
		off  []string
		want string
	}{
		{nil, "bootstrap / provision in its own tab"},
		{[]string{catalog.FeatureBootstrap, catalog.FeatureProvision}, "bootstrap / provision in its own tab (experimental, off)"},
		{[]string{catalog.FeatureProvision}, "bootstrap / provision in its own tab (provision off)"},
	} {
		m, _ := offModel(t, nil, tt.off...)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		press(m, keyPrefix, text("?")[0])
		var line string
		for _, l := range helpScreenLines(m) {
			if strings.Contains(l, "^B i/p") {
				line = strings.TrimSpace(ansi.Strip(l))
			}
		}
		if !strings.HasSuffix(line, tt.want) {
			t.Errorf("off %v: help line = %q, want it to end in %q", tt.off, line, tt.want)
		}
	}
}

func TestReloadPicksUpAFeatureTurnedOn(t *testing.T) {
	m, r := offModel(t, []Entry{{Alias: "alpha"}}, catalog.FeatureBootstrap)
	r.off = nil
	press(m, keyPrefix, text("r")[0])
	press(m, keyPrefix, text("i")[0])
	if want := (commandCall{"alpha", bootstrapLaunch}); len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", r.calls, want)
	}
}
