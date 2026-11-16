package workspace

import (
	"errors"
	"reflect"
	"testing"
)

func aliases[T Row | *Connection](rows []T) []string {
	out := []string{}
	for _, r := range rows {
		switch r := any(r).(type) {
		case *Connection:
			out = append(out, r.Alias)
		case Row:
			if !r.Header() {
				out = append(out, r.Conn.Alias)
			}
		}
	}
	return out
}

// rowNames lists rows with each group header as "[group]" ("[]" for ungrouped).
func rowNames(rows []Row) []string {
	out := []string{}
	for _, r := range rows {
		if r.Header() {
			out = append(out, "["+r.Group+"]")
		} else {
			out = append(out, r.Conn.Alias)
		}
	}
	return out
}

func sample() *State {
	return NewState([]Entry{
		{Alias: "staging", Group: "web"},
		{Alias: "prod-web", Description: "public site", Group: "web"},
		{Alias: "db-main", Description: "postgres"},
		{Alias: "old-box", Description: "retired", Orphaned: true},
	})
}

func TestGroupToOpenListsClosedLoadedMembersInListOrder(t *testing.T) {
	s := NewState([]Entry{
		{Alias: "web-2", Group: "web"},
		{Alias: "web-1", Group: "web"},
		{Alias: "web-open", Group: "web"},
		{Alias: "web-gone", Group: "web", Orphaned: true},
		{Alias: "db", Group: "data"},
		{Alias: "loose"},
	})
	mustOpen(t, s, "web-open")
	mustOpen(t, s, "db")
	s.SetFilter("loose") // the filter must not hide group members
	tests := []struct {
		group string
		want  []string
	}{
		{"web", []string{"web-1", "web-2"}},
		{"data", []string{}},
		{"", []string{}},
	}
	for _, tt := range tests {
		if got := aliases(s.GroupToOpen(tt.group)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("GroupToOpen(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
}

func TestRowsListGroupsThenUngroupedAndNeverReorderOnOpen(t *testing.T) {
	s := NewState([]Entry{
		{Alias: "zed", Group: "batch"},
		{Alias: "staging", Group: "web"},
		{Alias: "prod-web", Group: "web"},
		{Alias: "db-main"},
		{Alias: "old-box", Orphaned: true},
	})
	want := []string{"[batch]", "zed", "[web]", "prod-web", "staging", "[]", "db-main", "old-box"}
	if got := rowNames(s.Rows()); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	mustOpen(t, s, "staging")
	mustOpen(t, s, "db-main")
	if got := rowNames(s.Rows()); !reflect.DeepEqual(got, want) {
		t.Fatalf("opening reordered rows: %v, want %v", got, want)
	}
}

func TestRowsWithoutGroupsHaveNoHeaders(t *testing.T) {
	s := NewState([]Entry{{Alias: "b"}, {Alias: "a"}})
	s.ShowUngrouped(true) // there is no group to drag out of
	if got, want := rowNames(s.Rows()), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestCollapsedGroupHidesMembersAndKeepsCursorOnHeader(t *testing.T) {
	s := sample()
	s.SetCursor("staging")
	s.SetCollapsed("web", true)
	if got, want := rowNames(s.Rows()), []string{"[web]", "[]", "db-main", "old-box"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if r, ok := s.CursorRow(); !ok || !r.Header() || r.Group != "web" {
		t.Fatalf("cursor = %+v, want the web header", r)
	}
	if s.Cursor() != nil {
		t.Fatal("Cursor() must be nil on a header")
	}
	mustOpen(t, s, "prod-web") // opening a hidden member keeps the group collapsed
	if got := rowNames(s.Rows())[0:2]; !reflect.DeepEqual(got, []string{"[web]", "[]"}) {
		t.Fatalf("rows = %v", got)
	}
	if r, _ := s.CursorRow(); !r.Header() || s.Active().Alias != "prod-web" {
		t.Fatalf("cursor = %+v active = %s", r, s.Active().Alias)
	}
	s.SetCollapsed("web", false)
	if got := s.CollapsedGroups(); len(got) != 0 {
		t.Fatalf("collapsed = %v", got)
	}
}

func TestFilterShowsCollapsedMatchesAndDropsEmptyGroups(t *testing.T) {
	s := sample()
	s.SetCollapsed("web", true)
	s.SetFilter("public")
	if got, want := rowNames(s.Rows()), []string{"[web]", "prod-web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestSetGroupMovesConnectionBetweenGroups(t *testing.T) {
	s := sample()
	s.SetGroup("db-main", "web")
	s.SetGroup("staging", "")
	want := []string{"[web]", "db-main", "prod-web", "[]", "old-box", "staging"}
	if got := rowNames(s.Rows()); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	s.SetGroup("old-box", "web")
	s.SetGroup("staging", "web")
	if got := rowNames(s.Rows()); !reflect.DeepEqual(got, []string{"[web]", "db-main", "old-box", "prod-web", "staging"}) {
		t.Fatalf("empty ungrouped section should not be listed: %v", got)
	}
	s.ShowUngrouped(true)
	if got := rowNames(s.Rows()); got[len(got)-1] != "[]" {
		t.Fatalf("ShowUngrouped should list an empty ungrouped header: %v", got)
	}
}

func TestCycleActiveReachesConnectionsInCollapsedGroups(t *testing.T) {
	s := sample()
	mustOpen(t, s, "staging")
	mustOpen(t, s, "db-main")
	s.SetCollapsed("web", true)
	s.CycleActive(-1)
	if s.Active().Alias != "staging" {
		t.Fatalf("active = %s, want staging", s.Active().Alias)
	}
}

func TestOpenSessionNumbersTabsAndActivates(t *testing.T) {
	s := sample()
	a := mustOpen(t, s, "prod-web")
	b := mustOpen(t, s, "prod-web")
	if a.N != 1 || b.N != 2 {
		t.Fatalf("tab numbers = %d, %d", a.N, b.N)
	}
	if s.Active().Alias != "prod-web" || s.Active().CurrentSession() != b {
		t.Fatal("new tab should be current on the active connection")
	}
	s.SelectTab(-1)
	s.CloseCurrent() // closes tab 1
	c := mustOpen(t, s, "prod-web")
	if c.N != 3 {
		t.Fatalf("tab numbers must not be reused, got %d", c.N)
	}
}

func TestClosingLastTabStopsBeingOpen(t *testing.T) {
	s := sample()
	mustOpen(t, s, "db-main")
	mustOpen(t, s, "staging")
	s.Activate("db-main")
	s.CloseCurrent()
	if s.Get("db-main").Open() || s.Active() != nil {
		t.Fatal("db-main should be closed with no active connection")
	}
}

func TestEndedSessionKeepsConnectionOpen(t *testing.T) {
	s := sample()
	sess := mustOpen(t, s, "db-main")
	sess.Ended, sess.ExitCode = true, 255 // as the Session lifecycle does when its process exits
	c := s.Get("db-main")
	if !c.Open() || !c.AllEnded() || c.LiveSessions() != 0 || sess.ExitCode != 255 {
		t.Fatalf("unexpected state: open=%v allEnded=%v exit=%d", c.Open(), c.AllEnded(), sess.ExitCode)
	}
	mustOpen(t, s, "db-main")
	if c.AllEnded() || s.LiveSessions() != 1 {
		t.Fatal("a new live tab means not all ended")
	}
}

func TestOrphanedConnectionCannotOpen(t *testing.T) {
	s := sample()
	if _, err := s.OpenSession("old-box"); !errors.Is(err, ErrOrphaned) {
		t.Fatalf("err = %v, want ErrOrphaned", err)
	}
}

func TestFilterKeepsOpenConnectionsVisible(t *testing.T) {
	s := sample()
	mustOpen(t, s, "db-main")
	tests := []struct {
		filter string
		want   []string
	}{
		{"web", []string{"prod-web", "staging", "db-main"}}, // group match
		{"PUBLIC", []string{"prod-web", "db-main"}},         // description, case-insensitive
		{"zzz", []string{"db-main"}},
		{"", []string{"prod-web", "staging", "db-main", "old-box"}},
	}
	for _, tt := range tests {
		s.SetFilter(tt.filter)
		if got := aliases(s.Rows()); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("filter %q: rows = %v, want %v", tt.filter, got, tt.want)
		}
	}
}

func TestReloadKeepsOpenSessionsAndOrphansVanishedHosts(t *testing.T) {
	s := sample()
	mustOpen(t, s, "staging")
	s.Reload([]Entry{{Alias: "prod-web"}, {Alias: "new-host"}})
	st := s.Get("staging")
	if st == nil || !st.Orphaned || len(st.Sessions) != 1 {
		t.Fatalf("staging should stay open and become orphaned: %#v", st)
	}
	if s.Get("db-main") != nil {
		t.Fatal("closed connection missing from reload should disappear")
	}
	s.CloseCurrent()
	if s.Get("staging") != nil {
		t.Fatal("orphan without metadata should disappear once closed")
	}
	if got := aliases(s.Rows()); !reflect.DeepEqual(got, []string{"new-host", "prod-web"}) {
		t.Fatalf("rows = %v", got)
	}
}

func TestCursorAndCycleActive(t *testing.T) {
	s := sample()
	if r, _ := s.CursorRow(); !r.Header() || r.Group != "web" {
		t.Fatalf("cursor starts on the first row, the web header, got %+v", r)
	}
	s.MoveCursor(10)
	if s.Cursor().Alias != "old-box" {
		t.Fatalf("cursor clamps to last row, got %s", s.Cursor().Alias)
	}
	mustOpen(t, s, "prod-web")
	mustOpen(t, s, "staging")
	s.CycleActive(1)
	if s.Active().Alias != "prod-web" {
		t.Fatalf("cycle wraps to first open connection, got %s", s.Active().Alias)
	}
	s.CycleActive(-1)
	if s.Active().Alias != "staging" {
		t.Fatalf("cycle back, got %s", s.Active().Alias)
	}
}

func TestActivityMarksOnlySessionsNotShown(t *testing.T) {
	s := sample()
	db1 := mustOpen(t, s, "db-main")
	db2 := mustOpen(t, s, "db-main") // shown tab of db-main
	web := mustOpen(t, s, "staging")
	s.Activate("db-main") // staging is now in the background

	s.MarkActivity(db2)
	if db2.Unseen {
		t.Fatal("output in the shown tab must not mark it")
	}
	s.MarkActivity(db1)
	if !db1.Unseen || !s.Get("db-main").Unseen() {
		t.Fatal("output in a background tab marks the tab and its Connection")
	}
	s.MarkActivity(web)
	if !web.Unseen || !s.Get("staging").Unseen() {
		t.Fatal("output in a non-active Connection marks its tab and row")
	}
}

func TestShowingATabClearsItsActivity(t *testing.T) {
	tests := []struct {
		name string
		show func(s *State)
	}{
		{"select previous tab", func(s *State) { s.SelectTab(-1) }},
		{"select tab by index", func(s *State) { s.SelectTabIndex(0) }},
		{"close the shown tab", func(s *State) { s.CloseCurrent() }},
		{"activate its connection", func(s *State) { s.Activate("staging"); s.Activate("db-main"); s.SelectTab(-1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := sample()
			db1 := mustOpen(t, s, "db-main")
			mustOpen(t, s, "db-main")
			s.MarkActivity(db1)
			mustOpen(t, s, "staging")
			s.Activate("db-main")
			tt.show(s)
			if db1.Unseen || s.Get("db-main").Unseen() {
				t.Fatal("the shown tab and its row should be cleared")
			}
		})
	}
}

func TestActivatingConnectionClearsOnlyItsShownTab(t *testing.T) {
	s := sample()
	a1 := mustOpen(t, s, "db-main")
	a2 := mustOpen(t, s, "db-main")
	mustOpen(t, s, "staging")
	s.MarkActivity(a1)
	s.MarkActivity(a2)
	s.CycleActive(1) // db-main, showing tab 2
	if a2.Unseen || !a1.Unseen || !s.Get("db-main").Unseen() {
		t.Fatalf("a1=%v a2=%v: only the shown tab clears; the row stays marked", a1.Unseen, a2.Unseen)
	}
}

func mustOpen(t *testing.T, s *State, alias string) *Session {
	t.Helper()
	sess, err := s.OpenSession(alias)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestRowsListStoredGroupsInOrderIncludingEmptyOnes(t *testing.T) {
	s := NewState([]Entry{
		{Alias: "zed", Group: "batch"},
		{Alias: "web-1", Group: "web"},
		{Alias: "stray", Group: "adhoc"}, // no record: listed after the stored Groups
		{Alias: "loose"},
	})
	s.SetGroups([]string{"web", "spare", "batch"})
	want := []string{"[web]", "web-1", "[spare]", "[batch]", "zed", "[adhoc]", "stray", "[]", "loose"}
	if got := rowNames(s.Rows()); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	s.SetFilter("web")
	if got, want := rowNames(s.Rows()), []string{"[web]", "web-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered rows = %v, want %v", got, want)
	}
}

func TestOnlyEmptyGroupsStillShowHeaders(t *testing.T) {
	s := NewState([]Entry{{Alias: "a"}})
	s.SetGroups([]string{"spare"})
	if got, want := rowNames(s.Rows()), []string{"[spare]", "[]", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if !s.HasGroup("spare") || s.HasGroup("nowhere") || s.HasGroup("") {
		t.Fatal("HasGroup should know exactly the listed Groups")
	}
}

// What the user types for a Group is a Group number (its position among
// the stored Groups, 0 for ungrouped) or a name.
func TestResolveGroupTakesANumberOrAName(t *testing.T) {
	s := NewState([]Entry{{Alias: "stray", Group: "adhoc"}})
	s.SetGroups([]string{"prod", "staging"})
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"1", "prod", true},
		{"2", "staging", true},
		{"0", "", true},
		{"", "", true},
		{"3", "", false}, // adhoc has no record, so no number
		{"01", "prod", true},
		{"staging", "staging", true},
		{"new", "new", true},
		{"prod2", "prod2", true},
	}
	for _, tt := range tests {
		got, ok := s.ResolveGroup(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ResolveGroup(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
