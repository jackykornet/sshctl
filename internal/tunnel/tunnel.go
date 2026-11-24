// Package tunnel is the Tunnel: a saved local forward on a Connection
// (ADR-0012), its spec syntax and the rules for a Connection's list of them.
// The config stores Tunnels, openssh runs them, and the CLI and the
// Workspace's form read and write them as specs.
package tunnel

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Tunnel is a local forward: LocalPort on this machine to RemoteHost:RemotePort
// as the server sees it. Name is optional.
type Tunnel struct {
	Name       string `json:"name,omitempty"`
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
}

// DefaultHost is the remote host of a spec that names none: the server itself.
const DefaultHost = "localhost"

// SpecSyntax is how messages show a Tunnel spec.
const SpecSyntax = "[name=]localport[:host:remoteport]"

var (
	// ErrSpec is returned by Parse for a spec it refuses.
	ErrSpec = errors.New("invalid tunnel")
	// ErrDuplicate is returned by Add for a Tunnel whose local port or name
	// the Connection already has.
	ErrDuplicate = errors.New("duplicate tunnel")
	// ErrNoTunnel is returned by Remove when no Tunnel has the name or port.
	ErrNoTunnel = errors.New("no such tunnel")
)

// Parse reads a spec, [name=]localport[:host:remoteport]. "8083" forwards to
// localhost:8083 on the server. A name may not be only digits, so a name and
// a port never look alike, and may not hold spaces, "=" or ":".
func Parse(spec string) (Tunnel, error) {
	var t Tunnel
	rest := spec
	if name, after, ok := strings.Cut(spec, "="); ok {
		if err := checkName(name); err != nil {
			return Tunnel{}, err
		}
		t.Name, rest = name, after
	}
	local, target, hasTarget := strings.Cut(rest, ":")
	port, err := parsePort(local)
	if err != nil {
		return Tunnel{}, err
	}
	t.LocalPort, t.RemoteHost, t.RemotePort = port, DefaultHost, port
	if !hasTarget {
		return t, nil
	}
	i := strings.LastIndex(target, ":")
	if i < 0 {
		return Tunnel{}, fmt.Errorf("%w %q: want %s", ErrSpec, spec, SpecSyntax)
	}
	host := target[:i]
	if host == "" || strings.ContainsFunc(host, isSpace) {
		return Tunnel{}, fmt.Errorf("%w %q: the remote host is missing or has spaces", ErrSpec, spec)
	}
	if t.RemotePort, err = parsePort(target[i+1:]); err != nil {
		return Tunnel{}, err
	}
	t.RemoteHost = host
	return t, nil
}

func checkName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name before = is empty", ErrSpec)
	case strings.Trim(name, "0123456789") == "":
		return fmt.Errorf("%w name %q: it is only digits, which stand for ports", ErrSpec, name)
	case strings.ContainsFunc(name, isSpace) || strings.ContainsAny(name, "=:"):
		return fmt.Errorf("%w name %q: it may not hold spaces, = or :", ErrSpec, name)
	}
	return nil
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 || strings.Trim(s, "0123456789") != "" {
		return 0, fmt.Errorf("%w port %q: want a number from 1 to 65535", ErrSpec, s)
	}
	return n, nil
}

// String is t's spec, in the shortest form Parse reads back.
func (t Tunnel) String() string {
	var b strings.Builder
	if t.Name != "" {
		b.WriteString(t.Name + "=")
	}
	b.WriteString(strconv.Itoa(t.LocalPort))
	if t.RemoteHost != DefaultHost || t.RemotePort != t.LocalPort {
		fmt.Fprintf(&b, ":%s:%d", t.RemoteHost, t.RemotePort)
	}
	return b.String()
}

// Forward is t as ssh -L takes it: localport:host:remoteport.
func (t Tunnel) Forward() string {
	return fmt.Sprintf("%d:%s:%d", t.LocalPort, t.RemoteHost, t.RemotePort)
}

// Label is how t is named to the user: its name, or else its local port.
func (t Tunnel) Label() string {
	if t.Name != "" {
		return t.Name
	}
	return strconv.Itoa(t.LocalPort)
}

// Add returns list with t appended, or ErrDuplicate when a Tunnel in list
// already has t's local port or name. list is not changed.
func Add(list []Tunnel, t Tunnel) ([]Tunnel, error) {
	for _, have := range list {
		if have.LocalPort == t.LocalPort {
			return nil, fmt.Errorf("%w: local port %d is already forwarded", ErrDuplicate, t.LocalPort)
		}
		if t.Name != "" && have.Name == t.Name {
			return nil, fmt.Errorf("%w: a tunnel is already named %q", ErrDuplicate, t.Name)
		}
	}
	return append(slices.Clip(list), t), nil
}

// Remove returns list without the Tunnel ref names (a name, or a local port
// when ref is only digits) and that Tunnel. It returns ErrNoTunnel when none
// matches. list is not changed.
func Remove(list []Tunnel, ref string) ([]Tunnel, Tunnel, error) {
	port, err := strconv.Atoi(ref)
	byPort := err == nil
	i := slices.IndexFunc(list, func(t Tunnel) bool {
		if byPort {
			return t.LocalPort == port
		}
		return ref != "" && t.Name == ref
	})
	if i < 0 {
		return nil, Tunnel{}, fmt.Errorf("%w %q", ErrNoTunnel, ref)
	}
	return slices.Delete(slices.Clone(list), i, i+1), list[i], nil
}
