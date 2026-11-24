package tunnel

import (
	"errors"
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want Tunnel
		err  bool
	}{
		{spec: "8083", want: Tunnel{LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}},
		{spec: "panel=8080:10.0.0.5:80", want: Tunnel{Name: "panel", LocalPort: 8080, RemoteHost: "10.0.0.5", RemotePort: 80}},
		{spec: "9000:db.internal:5432", want: Tunnel{LocalPort: 9000, RemoteHost: "db.internal", RemotePort: 5432}},
		{spec: "admin=8083", want: Tunnel{Name: "admin", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}},
		{spec: "v6=8080:[::1]:80", want: Tunnel{Name: "v6", LocalPort: 8080, RemoteHost: "[::1]", RemotePort: 80}},
		{spec: "1", want: Tunnel{LocalPort: 1, RemoteHost: "localhost", RemotePort: 1}},
		{spec: "65535", want: Tunnel{LocalPort: 65535, RemoteHost: "localhost", RemotePort: 65535}},
		{spec: "", err: true},
		{spec: "0", err: true},
		{spec: "65536", err: true},
		{spec: "-1", err: true},
		{spec: "web", err: true},
		{spec: "8080:host", err: true},
		{spec: "8080::80", err: true},
		{spec: "8080:host:0", err: true},
		{spec: "8080:host:70000", err: true},
		{spec: "8080:host:http", err: true},
		{spec: "=8080", err: true},
		{spec: "123=8080", err: true},
		{spec: "my panel=8080", err: true},
		{spec: "a:b=8080", err: true},
		{spec: "8080:::1:80", err: true},
		{spec: "8080:[::1:80", err: true},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := Parse(tc.spec)
			if tc.err {
				if !errors.Is(err, ErrSpec) {
					t.Fatalf("Parse(%q) = %+v, %v; want an ErrSpec", tc.spec, got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("Parse(%q) = %+v, %v; want %+v", tc.spec, got, err, tc.want)
			}
		})
	}
}

func TestStringRoundTrips(t *testing.T) {
	for _, spec := range []string{"8083", "panel=8080:10.0.0.5:80", "9000:localhost:5432", "admin=8083"} {
		tun, err := Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := tun.String(); got != spec {
			t.Errorf("Parse(%q).String() = %q", spec, got)
		}
	}
}

func TestAdd(t *testing.T) {
	existing := []Tunnel{
		{Name: "panel", LocalPort: 8080, RemoteHost: "localhost", RemotePort: 80},
		{LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083},
	}
	for _, tc := range []struct {
		name string
		add  Tunnel
		err  bool
	}{
		{"new port", Tunnel{LocalPort: 9000, RemoteHost: "localhost", RemotePort: 9000}, false},
		{"new name", Tunnel{Name: "db", LocalPort: 5433, RemoteHost: "localhost", RemotePort: 5432}, false},
		{"same local port", Tunnel{Name: "other", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 80}, true},
		{"same name", Tunnel{Name: "panel", LocalPort: 9001, RemoteHost: "localhost", RemotePort: 80}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Add(existing, tc.add)
			if tc.err {
				if !errors.Is(err, ErrDuplicate) || got != nil {
					t.Fatalf("Add = %v, %v; want ErrDuplicate", got, err)
				}
				return
			}
			if err != nil || len(got) != 3 || got[2] != tc.add {
				t.Fatalf("Add = %v, %v; want it appended", got, err)
			}
			if len(existing) != 2 {
				t.Fatal("Add changed its argument")
			}
		})
	}
}

func TestRemove(t *testing.T) {
	list := []Tunnel{
		{Name: "panel", LocalPort: 8080, RemoteHost: "localhost", RemotePort: 80},
		{LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083},
	}
	for _, tc := range []struct {
		ref  string
		want []Tunnel
	}{
		{"panel", list[1:]},
		{"8080", list[1:]},
		{"8083", list[:1]},
	} {
		got, removed, err := Remove(list, tc.ref)
		if err != nil || len(got) != len(tc.want) || got[0] != tc.want[0] || slices.Contains(got, removed) || !slices.Contains(list, removed) {
			t.Errorf("Remove(%q) = %v, %+v, %v; want %v", tc.ref, got, removed, err, tc.want)
		}
	}
	for _, ref := range []string{"db", "9000", "", "+8080"} {
		if _, _, err := Remove(list, ref); !errors.Is(err, ErrNoTunnel) {
			t.Errorf("Remove(%q) err = %v, want ErrNoTunnel", ref, err)
		}
	}
	if len(list) != 2 || list[0].Name != "panel" {
		t.Error("Remove changed its argument")
	}
}

func TestParseList(t *testing.T) {
	panel := Tunnel{Name: "panel", LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}
	cockpit := Tunnel{Name: "cockpit", LocalPort: 9090, RemoteHost: "localhost", RemotePort: 9090}
	db := Tunnel{LocalPort: 5433, RemoteHost: "10.0.0.5", RemotePort: 5432}
	for _, tc := range []struct {
		text string
		want []Tunnel
		err  error
	}{
		{"", nil, nil},
		{"  ", nil, nil},
		{"panel=8083, cockpit=9090", []Tunnel{panel, cockpit}, nil},
		{"panel=8083,5433:10.0.0.5:5432,", []Tunnel{panel, db}, nil},
		{"panel=8083, 70000", nil, ErrSpec},
		{"8083, panel=8083", nil, ErrDuplicate},
		{"panel=8083, panel=9090", nil, ErrDuplicate},
	} {
		got, err := ParseList(tc.text)
		if !errors.Is(err, tc.err) || !slices.Equal(got, tc.want) {
			t.Errorf("ParseList(%q) = %v, %v; want %v, %v", tc.text, got, err, tc.want, tc.err)
		}
	}
}

func TestJoinRoundTrips(t *testing.T) {
	const text = "panel=8083, 5433:10.0.0.5:5432"
	list, err := ParseList(text)
	if err != nil {
		t.Fatal(err)
	}
	if got := Join(list); got != text {
		t.Errorf("Join = %q, want %q", got, text)
	}
	if got := Join(nil); got != "" {
		t.Errorf("Join(nil) = %q", got)
	}
}

func TestCheck(t *testing.T) {
	a := Tunnel{LocalPort: 8083, RemoteHost: "localhost", RemotePort: 8083}
	if err := Check([]Tunnel{a, {Name: "b", LocalPort: 9000, RemoteHost: "localhost", RemotePort: 80}}); err != nil {
		t.Errorf("Check = %v, want nil", err)
	}
	if err := Check([]Tunnel{a, a}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("Check(duplicate) = %v, want ErrDuplicate", err)
	}
	if err := Check([]Tunnel{{LocalPort: 0, RemoteHost: "localhost", RemotePort: 80}}); !errors.Is(err, ErrSpec) {
		t.Errorf("Check(port 0) = %v, want ErrSpec", err)
	}
}
