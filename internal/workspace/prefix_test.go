package workspace

import "testing"

// Prefix c is unbound: it starts no Session and gets the hint any unbound
// prefix key gets.
func TestPrefixCIsUnbound(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}, {Alias: "beta"}})
	press(m, keyPrefix, text("c")[0])
	if len(r.calls) != 0 || m.st.Active() != nil {
		t.Fatalf("calls = %v active = %v, want no Session", r.calls, m.st.Active())
	}
	if want := "unknown command ^B c · ^B ? help"; m.status != want {
		t.Fatalf("status = %q, want %q", m.status, want)
	}
}
