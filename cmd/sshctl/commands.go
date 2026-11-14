package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/platform"
)

type app struct {
	in       io.Reader
	out, err io.Writer
	cfgPath  string
	legacy   []string
	cfg      *config.Config
	ssh      *openssh.Client
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	startWorkspace := len(args) == 0 && interactive(in, out)
	if !startWorkspace && (len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		help(out)
		return nil
	}
	p, legacy, err := config.Paths()
	if err != nil {
		return err
	}
	c, err := config.Load(p, legacy)
	if err != nil {
		return err
	}
	s, err := openssh.New()
	if err != nil {
		return err
	}
	a := &app{in: in, out: out, err: errOut, cfgPath: p, legacy: legacy, cfg: c, ssh: s}
	if startWorkspace {
		return a.runWorkspace()
	}
	return a.execute(args)
}
func help(w io.Writer) {
	fmt.Fprint(w, `sshctl manages OpenSSH connections and local connection metadata.

Usage: sshctl                 Open the Workspace (in a terminal)
       sshctl <command> [arguments]

Commands:
  list                         List saved connections and SSH config aliases
  connect <alias> [--claude]   Connect using OpenSSH
  add <alias>                  Add or update connection metadata
  edit <alias>                 Edit connection metadata
  remove <alias>               Remove sshctl metadata
  group connect <group>        Open group connections in terminal windows
  history                      Show recent connections
  key list|use|add             List, select, or register keys
  agent ensure                 Inspect SSH agent and optionally add default key
  known-hosts list|remove      Show or remove known hosts
  config show                  Show paths and relevant SSH settings

Use sshctl <command> --help for command usage where available.
`)
}
func usage(command string) string {
	switch command {
	case "connect":
		return "connect <alias> [--claude]"
	case "add", "edit", "remove":
		return command + " <alias>"
	case "group":
		return "group connect <group>"
	case "key":
		return "key list | use <key-path|none> | add <private-key-path>"
	case "agent":
		return "agent ensure"
	case "known-hosts":
		return "known-hosts list | remove <host>"
	case "config":
		return "config show"
	default:
		return command
	}
}

func (a *app) execute(args []string) error {
	if args[0] == "--help" || args[0] == "-h" {
		help(a.out)
		return nil
	}
	if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Fprintf(a.out, "Usage: sshctl %s\n", usage(args[0]))
		return nil
	}
	switch args[0] {
	case "list":
		return a.list()
	case "connect":
		return a.connect(args[1:])
	case "add":
		return a.metadata(args[1:], false)
	case "edit":
		return a.metadata(args[1:], true)
	case "remove":
		return a.remove(args[1:])
	case "group":
		if len(args) > 1 && args[1] == "connect" {
			return a.groupConnect(args[2:])
		}
	case "history":
		for _, h := range a.cfg.History {
			fmt.Fprintln(a.out, h)
		}
		return nil
	case "key":
		return a.keys(args[1:])
	case "agent":
		if len(args) > 1 && args[1] == "ensure" {
			return a.agentEnsure()
		}
	case "known-hosts":
		return a.knownHosts(args[1:])
	case "config":
		if len(args) > 1 && args[1] == "show" {
			return a.showConfig()
		}
	}
	return fmt.Errorf("unknown or incomplete command; run sshctl --help")
}
func (a *app) save() error { return config.Save(a.cfgPath, a.cfg) }
func (a *app) list() error {
	aliases, err := a.ssh.Aliases()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, name := range aliases {
		seen[name] = true
		c, ok := a.cfg.Connections[name]
		label := ""
		if ok {
			label = c.Description
			if c.Group != "" {
				label += fmt.Sprintf(" [%s]", c.Group)
			}
		}
		fmt.Fprintf(a.out, "%-24s %s\n", name, label)
	}
	for name, c := range a.cfg.Connections {
		if !seen[name] {
			fmt.Fprintf(a.out, "%-24s %s (saved)\n", name, c.Description)
		}
	}
	return nil
}
func (a *app) connect(args []string) error {
	claude := false
	pos := []string{}
	for _, arg := range args {
		if arg == "--claude" {
			claude = true
		} else {
			pos = append(pos, arg)
		}
	}
	if len(pos) != 1 {
		return errors.New("usage: sshctl connect <alias> [--claude]")
	}
	alias := pos[0]
	if err := a.ssh.Connect(context.Background(), alias, a.cfg.DefaultKey, claude); err != nil {
		return err
	}
	return a.recordHistory(alias)
}

// recordHistory moves alias to the front of History, keeping the 20 most recent.
func (a *app) recordHistory(alias string) error {
	a.cfg.RecordOpen(alias)
	return a.save()
}
func (a *app) metadata(args []string, edit bool) error {
	if len(args) != 1 {
		return errors.New("usage: sshctl add|edit <alias>")
	}
	alias := args[0]
	c, exists := a.cfg.Connections[alias]
	if edit && !exists {
		return fmt.Errorf("no sshctl metadata for %q", alias)
	}
	c.Alias = alias
	reader := bufio.NewReader(a.in)
	fmt.Fprintf(a.out, "Description [%s]: ", c.Description)
	desc, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(desc) != "" {
		c.Description = strings.TrimSpace(desc)
	}
	fmt.Fprintf(a.out, "Group [%s]: ", c.Group)
	group, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(group) != "" {
		c.Group = strings.TrimSpace(group)
	}
	a.cfg.Connections[alias] = c
	return a.save()
}
func (a *app) remove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: sshctl remove <alias>")
	}
	if _, ok := a.cfg.Connections[args[0]]; !ok {
		return fmt.Errorf("no sshctl metadata for %q", args[0])
	}
	delete(a.cfg.Connections, args[0])
	return a.save()
}
func (a *app) groupConnect(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: sshctl group connect <group>")
	}
	var names []string
	for n, c := range a.cfg.Connections {
		if c.Group == args[0] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("group %q has no saved connections", args[0])
	}
	for _, n := range names {
		if err := platform.OpenSSH(n, a.cfg.DefaultKey); err != nil {
			return err
		}
	}
	return nil
}
func (a *app) keys(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl key list|use|add")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	keyDir := filepath.Join(home, ".ssh")
	switch args[0] {
	case "list":
		set := map[string]bool{}
		entries, _ := os.ReadDir(keyDir)
		for _, e := range entries {
			n := e.Name()
			if strings.HasSuffix(n, ".pub") || strings.HasSuffix(n, "-cert.pub") {
				set[filepath.Join(keyDir, n)] = true
			}
		}
		for _, k := range a.cfg.Keys {
			set[k] = true
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			tag := ""
			if k == a.cfg.DefaultKey || strings.TrimSuffix(k, ".pub") == a.cfg.DefaultKey {
				tag = " (default)"
			}
			fmt.Fprintln(a.out, k+tag)
		}
		return nil
	case "add":
		if len(args) != 2 {
			return errors.New("usage: sshctl key add <private-key-path>")
		}
		abs, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		if _, err = os.Stat(abs); err != nil {
			return err
		}
		for _, k := range a.cfg.Keys {
			if k == abs {
				return nil
			}
		}
		a.cfg.Keys = append(a.cfg.Keys, abs)
		return a.save()
	case "use":
		if len(args) != 2 {
			return errors.New("usage: sshctl key use <key-path|none>")
		}
		k := args[1]
		if k != "none" {
			abs, err := filepath.Abs(k)
			if err != nil {
				return err
			}
			k = abs
			if _, err = os.Stat(k); err != nil {
				return err
			}
		} else {
			k = ""
		}
		a.cfg.DefaultKey = k
		return a.save()
	}
	return errors.New("usage: sshctl key list|use|add")
}
func (a *app) agentEnsure() error {
	status, err := openssh.AgentStatus()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "SSH agent:", status)
	if a.cfg.DefaultKey == "" {
		fmt.Fprintln(a.out, "No sshctl default key selected; OpenSSH config identities remain in effect.")
		return nil
	}
	loaded, checkable, err := openssh.KeyLoaded(a.cfg.DefaultKey)
	if err != nil {
		return err
	}
	if loaded {
		fmt.Fprintln(a.out, "Selected key is already loaded.")
		return nil
	}
	if !checkable {
		fmt.Fprintln(a.out, "No adjacent .pub file; loaded status cannot be checked without unlocking the private key.")
	}
	fmt.Fprintf(a.out, "Add default key %q to the agent? [y/N] ", a.cfg.DefaultKey)
	if !confirm(a.in) {
		return nil
	}
	return openssh.AddKey(a.cfg.DefaultKey)
}
func (a *app) knownHosts(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl known-hosts list|remove <host>")
	}
	path := openssh.KnownHostsPath()
	switch args[0] {
	case "list":
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				parts := strings.Fields(line)
				if len(parts) > 0 {
					fmt.Fprintln(a.out, parts[0])
				}
			}
		}
		return sc.Err()
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: sshctl known-hosts remove <host>")
		}
		fmt.Fprintf(a.out, "Remove known host %q? [y/N] ", args[1])
		if !confirm(a.in) {
			return nil
		}
		return openssh.RemoveKnownHost(args[1], path)
	}
	return errors.New("usage: sshctl known-hosts list|remove <host>")
}
func confirm(in io.Reader) bool {
	line, err := bufio.NewReader(in).ReadString('\n')
	return err == nil && (strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes"))
}
func (a *app) showConfig() error {
	fmt.Fprintf(a.out, "sshctl config: %s\nOpenSSH config: %s\nKnown hosts: %s\nDefault key override: %s\n", a.cfgPath, a.ssh.SSHConfig, openssh.KnownHostsPath(), a.cfg.DefaultKey)
	settings, err := a.ssh.SafeSettings()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "OpenSSH settings:")
	for _, setting := range settings {
		fmt.Fprintln(a.out, "  "+setting)
	}
	return nil
}
