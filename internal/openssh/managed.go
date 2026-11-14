package openssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ManagedFile is the file, relative to the ssh directory, that holds the Host
// blocks sshctl writes. The user's ssh_config must Include it.
const ManagedFile = "sshctl_config"

// IncludeLine is the directive that makes OpenSSH read ManagedFile.
const IncludeLine = "Include " + ManagedFile

const managedHeader = "# Managed by sshctl. Add, edit and delete these hosts in the sshctl Workspace.\n" +
	"# Other options you add inside a Host block are kept; comments are not."

// Endpoint is how OpenSSH reaches a host: the directives sshctl writes in a
// Host block.
type Endpoint struct {
	HostName     string
	User         string
	Port         string
	IdentityFile string
}

// Host is one Host block in the managed file.
type Host struct {
	Alias string
	Endpoint
	// Extra holds the block's other directive lines, kept as written.
	Extra []string
}

// ErrUnknownHost is returned when a managed Host block does not exist.
var ErrUnknownHost = errors.New("not a host managed by sshctl")

// AliasExistsError is returned when saving would add an alias that ssh_config
// already defines. OpenSSH uses the first block it reads, so a second block
// with the same alias would silently win or lose.
type AliasExistsError struct{ Alias string }

func (e *AliasExistsError) Error() string { return e.Alias + " already exists in ssh_config" }

// ErrNotIncluded is returned when saving a Host block while the user's
// ssh_config does not Include the managed file, so OpenSSH would not read
// it. AddInclude fixes it.
var ErrNotIncluded = errors.New(MissingInclude("ssh_config", IncludeLine))

// MissingInclude says that the ssh_config file config lacks the Include
// directive line, as ErrNotIncluded does.
func MissingInclude(config, line string) string { return config + " does not contain " + line }

func (c *Client) sshDir() string {
	if c.SSHDir != "" {
		return c.SSHDir
	}
	return filepath.Dir(c.SSHConfig)
}

// ManagedPath is the full path of the managed file.
func (c *Client) ManagedPath() string { return filepath.Join(c.sshDir(), ManagedFile) }

// ManagedHosts reads the managed file's Host blocks in file order. A missing
// file has no hosts.
func (c *Client) ManagedHosts() ([]Host, error) {
	b, err := os.ReadFile(c.ManagedPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var hosts []Host
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		keyword, args := splitQuoted(line)
		if keyword == "" {
			continue
		}
		if strings.EqualFold(keyword, "host") {
			alias := ""
			if len(args) > 0 {
				alias = args[0]
			}
			hosts = append(hosts, Host{Alias: alias})
			continue
		}
		if len(hosts) == 0 {
			continue // directives before the first Host apply to every host; sshctl writes none
		}
		h := &hosts[len(hosts)-1]
		value := strings.Join(args, " ")
		switch strings.ToLower(keyword) {
		case "hostname":
			h.HostName = value
		case "user":
			h.User = value
		case "port":
			h.Port = value
		case "identityfile":
			h.IdentityFile = value
		default:
			h.Extra = append(h.Extra, strings.TrimSpace(line))
		}
	}
	return hosts, nil
}

// SaveHost writes h to the managed file, replacing the block for old, or
// adding a new block when old is "". Extra lines of a replaced block are kept.
//
// It enforces ADR-0002: old must be a block in the managed file
// (ErrUnknownHost otherwise), h must pass Validate, a new or renamed alias
// must not exist anywhere in ssh_config (*AliasExistsError), and the user's
// ssh_config must Include the managed file (ErrNotIncluded).
func (c *Client) SaveHost(old string, h Host) error {
	hosts, err := c.ManagedHosts()
	if err != nil {
		return err
	}
	i := -1
	if old != "" {
		i = slices.IndexFunc(hosts, func(x Host) bool { return x.Alias == old })
		if i < 0 {
			return fmt.Errorf("%s: %w", old, ErrUnknownHost)
		}
	}
	h.IdentityFile = HomeRelative(h.IdentityFile, c.home())
	if err := h.Validate(); err != nil {
		return err
	}
	if old != h.Alias {
		aliases, err := c.Aliases()
		if err != nil {
			return err
		}
		inManaged := slices.ContainsFunc(hosts, func(x Host) bool { return x.Alias == h.Alias })
		if inManaged || slices.Contains(aliases, h.Alias) {
			return &AliasExistsError{Alias: h.Alias}
		}
	}
	included, err := c.IncludesManaged()
	if err != nil {
		return err
	}
	if !included {
		return ErrNotIncluded
	}
	if i < 0 {
		hosts = append(hosts, h)
	} else {
		h.Extra = hosts[i].Extra
		hosts[i] = h
	}
	return c.writeManaged(hosts)
}

func (c *Client) home() string {
	if c.Home != "" {
		return c.Home
	}
	home, _ := os.UserHomeDir()
	return home
}

// HomeRelative returns path as "~/…" when it is an absolute path inside
// home, and unchanged otherwise. OpenSSH expands "~" on Windows, Linux and
// macOS, so a Host block synced between machines keeps naming the same key
// (ADR-0004).
//
// A Windows home (one with a drive letter or a \\server share) matches
// regardless of case and of / versus \; any other home matches exactly. The
// result always uses /, which OpenSSH reads on Windows too.
func HomeRelative(path, home string) string {
	windows := len(home) >= 2 && home[1] == ':' || strings.HasPrefix(home, `\\`)
	p := path
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
		home = strings.ReplaceAll(home, `\`, "/")
	}
	home = strings.TrimRight(home, "/")
	if home == "" || len(p) <= len(home)+1 || p[len(home)] != '/' {
		return path
	}
	if prefix := p[:len(home)]; prefix != home && !(windows && strings.EqualFold(prefix, home)) {
		return path
	}
	return "~/" + p[len(home)+1:]
}

// DeleteHost removes alias's block from the managed file.
func (c *Client) DeleteHost(alias string) error {
	hosts, err := c.ManagedHosts()
	if err != nil {
		return err
	}
	for i, h := range hosts {
		if h.Alias == alias {
			return c.writeManaged(append(hosts[:i], hosts[i+1:]...))
		}
	}
	return fmt.Errorf("%s: %w", alias, ErrUnknownHost)
}

func (c *Client) writeManaged(hosts []Host) error {
	var b strings.Builder
	b.WriteString(managedHeader + "\n")
	for _, h := range hosts {
		b.WriteString("\nHost " + h.Alias + "\n")
		for _, kv := range [][2]string{{"HostName", h.HostName}, {"User", h.User}, {"Port", h.Port}, {"IdentityFile", h.IdentityFile}} {
			if kv[1] == "" {
				continue
			}
			v := kv[1]
			if strings.ContainsAny(v, " \t") {
				v = `"` + v + `"`
			}
			b.WriteString("    " + kv[0] + " " + v + "\n")
		}
		for _, x := range h.Extra {
			b.WriteString("    " + x + "\n")
		}
	}
	return writeAtomic(c.ManagedPath(), []byte(b.String()), 0o600)
}

// Validate checks that h can be written as a Host block that OpenSSH reads
// back unchanged, so no value can smuggle in another directive.
func (h Host) Validate() error {
	if h.Alias == "" {
		return errors.New("alias is required")
	}
	if strings.ContainsAny(h.Alias, " \t*?!#\"=,") || strings.HasPrefix(h.Alias, "-") {
		return fmt.Errorf("alias %q must be one word without * ? ! # \" = , or a leading -", h.Alias)
	}
	if h.HostName == "" {
		return errors.New("hostname is required")
	}
	for _, f := range [][2]string{{"hostname", h.HostName}, {"user", h.User}} {
		if strings.ContainsAny(f[1], " \t#\"=") {
			return fmt.Errorf("%s %q must not contain spaces, #, \" or =", f[0], f[1])
		}
	}
	if h.Port != "" {
		if p, err := strconv.Atoi(h.Port); err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("port %q must be a number from 1 to 65535", h.Port)
		}
	}
	if strings.ContainsAny(h.IdentityFile, "#\"") {
		return fmt.Errorf("identity file %q must not contain # or \"", h.IdentityFile)
	}
	for _, v := range []string{h.Alias, h.HostName, h.User, h.Port, h.IdentityFile} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("values must be on one line")
		}
	}
	return nil
}

// IncludesManaged reports whether the user's ssh_config Includes the managed
// file before its first Host or Match block, where it applies to every host.
func (c *Client) IncludesManaged() (bool, error) {
	b, err := os.ReadFile(c.SSHConfig)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	want := filepath.Clean(c.ManagedPath())
	for _, line := range strings.Split(string(b), "\n") {
		keyword, args := splitDirective(line)
		switch strings.ToLower(keyword) {
		case "host", "match":
			return false, nil
		case "include":
			for _, a := range args {
				if ok, _ := filepath.Match(filepath.Clean(expandIncludePath(a, c.sshDir())), want); ok {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// AddInclude puts IncludeLine at the top of the user's ssh_config, creating
// the file if needed. The previous file is kept next to it as config.sshctl-backup.
func (c *Client) AddInclude() error {
	old, err := os.ReadFile(c.SSHConfig)
	mode := os.FileMode(0o600)
	switch {
	case err == nil:
		if fi, err := os.Stat(c.SSHConfig); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := writeAtomic(c.SSHConfig+".sshctl-backup", old, mode); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return err
	}
	head := "# Added by sshctl: read the hosts sshctl manages.\n" + IncludeLine + "\n"
	if len(old) > 0 {
		head += "\n"
	}
	return writeAtomic(c.SSHConfig, append([]byte(head), old...), mode)
}

// writeAtomic replaces path through a temporary file in the same directory,
// so an interrupted write never leaves it truncated.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sshctl-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// splitQuoted is splitDirective with double-quoted arguments, which OpenSSH
// accepts for values containing spaces.
func splitQuoted(line string) (string, []string) {
	keyword, _ := splitDirective(line)
	if keyword == "" {
		return "", nil
	}
	line = strings.TrimSpace(line)
	rest := strings.TrimLeft(line[len(keyword):], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	var args []string
	for rest != "" && rest[0] != '#' {
		if rest[0] == '"' {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				args = append(args, rest[1:])
				break
			}
			args = append(args, rest[1:end+1])
			rest = rest[end+2:]
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			args = append(args, rest[:end])
			rest = rest[end:]
		}
		rest = strings.TrimLeft(rest, " \t")
	}
	return keyword, args
}
