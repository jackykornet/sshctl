package catalog

import (
	"errors"
	"strings"
	"testing"
)

// resolveTree is a catalog with Spaces home (the default, 1), acme (2) and
// globex (4, so 3 is free). home has Group prod; acme has prod and data;
// globex has none.
func resolveTree(t *testing.T) Snapshot {
	t.Helper()
	tr := newTree(t, true, "")
	for _, op := range []func() error{
		func() error { return tr.cat.RenameSpace("", "home") },
		func() error { return tr.cat.AddSpace("acme") },
		func() error { return tr.cat.AddSpaceAt("globex", 4) },
		func() error { return tr.cat.AddGroup("", "prod") },
		func() error { return tr.cat.AddGroup("acme", "prod") },
		func() error { return tr.cat.AddGroup("acme", "data") },
	} {
		if err := op(); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestResolveSpaceTakesANameOrASpaceNumber(t *testing.T) {
	snap := resolveTree(t)
	for _, tc := range []struct {
		in      string
		want    string
		wantErr string // a part of the error, "" for none
	}{
		{in: "home", want: ""},
		{in: "acme", want: "acme"},
		{in: "1", want: ""},
		{in: "2", want: "acme"},
		{in: "4", want: "globex"},
		{in: "3", wantErr: "no Space 3; the Spaces are 1 home, 2 acme, 4 globex"},
		{in: "0", wantErr: "no Space 0"},
		{in: "99999999999999999999", wantErr: "no Space 99999999999999999999"},
		{in: "nowhere", wantErr: `no Space "nowhere"; the Spaces are 1 home, 2 acme, 4 globex`},
		{in: "", wantErr: `no Space ""`},
	} {
		got, err := snap.ResolveSpace(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !errors.Is(err, ErrUnknownSpace) {
				t.Errorf("ResolveSpace(%q) = %q, %v; want an ErrUnknownSpace saying %q", tc.in, got, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ResolveSpace(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestResolveGroupTakesANameAGroupNumberOrZero(t *testing.T) {
	snap := resolveTree(t)
	for _, tc := range []struct {
		space, in string
		want      string
		wantErr   string
		is        error
	}{
		{space: "acme", in: "data", want: "data"},
		{space: "acme", in: "1", want: "prod"},
		{space: "acme", in: "2", want: "data"},
		{space: "acme", in: "0", want: ""},
		{space: "globex", in: "0", want: ""},
		{space: "", in: "1", want: "prod"},
		{space: "acme", in: "3", wantErr: "no group number 3 in Space acme; its groups are 1 prod, 2 data", is: ErrUnknownGroupNumber},
		{space: "globex", in: "1", wantErr: "no group number 1 in Space globex; it has no groups", is: ErrUnknownGroupNumber},
		{space: "acme", in: "99999999999999999999", wantErr: "no group number 99999999999999999999", is: ErrUnknownGroupNumber},
		{space: "", in: "data", wantErr: `no group "data" in Space home; its groups are 1 prod`, is: ErrUnknownGroup},
	} {
		got, err := snap.ResolveGroup(tc.space, tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !errors.Is(err, tc.is) {
				t.Errorf("ResolveGroup(%q, %q) = %q, %v; want %v saying %q", tc.space, tc.in, got, err, tc.is, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ResolveGroup(%q, %q) = %q, %v; want %q", tc.space, tc.in, got, err, tc.want)
		}
	}
}

func TestFindGroupLooksInTheOneSpaceThatHasIt(t *testing.T) {
	snap := resolveTree(t)
	for _, tc := range []struct {
		in        string
		wantSpace string
		want      string
		wantErr   string
		is        error
	}{
		{in: "data", wantSpace: "acme", want: "data"},
		{in: "prod", wantErr: `group "prod" is in Spaces home, acme`, is: ErrGroupInSeveralSpaces},
		{in: "2", wantErr: "group number 2 counts within a Space, and there are 3 Spaces", is: ErrGroupInSeveralSpaces},
		{in: "0", wantErr: "group number 0 counts within a Space", is: ErrGroupInSeveralSpaces},
		{in: "nowhere", wantErr: `no group "nowhere" in any Space; the groups are home: prod; acme: prod, data`, is: ErrUnknownGroup},
	} {
		space, got, err := snap.FindGroup(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !errors.Is(err, tc.is) {
				t.Errorf("FindGroup(%q) = %q, %q, %v; want %v saying %q", tc.in, space, got, err, tc.is, tc.wantErr)
			}
			continue
		}
		if err != nil || space != tc.wantSpace || got != tc.want {
			t.Errorf("FindGroup(%q) = %q, %q, %v; want %q, %q", tc.in, space, got, err, tc.wantSpace, tc.want)
		}
	}
}

func TestFindGroupWithOneSpaceTakesNumbersAndZero(t *testing.T) {
	tr := newTree(t, true, "")
	for _, name := range []string{"prod", "data"} {
		if err := tr.cat.AddGroup("", name); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := tr.cat.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{"2": "data", "0": "", "prod": "prod"} {
		if space, got, err := snap.FindGroup(in); err != nil || space != "" || got != want {
			t.Errorf("FindGroup(%q) = %q, %q, %v; want \"\", %q", in, space, got, err, want)
		}
	}
	if _, _, err := snap.FindGroup("3"); !errors.Is(err, ErrUnknownGroupNumber) {
		t.Errorf("FindGroup(3) = %v, want ErrUnknownGroupNumber", err)
	}
}
