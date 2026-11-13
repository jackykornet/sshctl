package openssh

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Client struct {
	SSHConfig string
	SSHDir    string
}

func New() (*Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Client{SSHConfig: filepath.Join(home, ".ssh", "config"), SSHDir: filepath.Join(home, ".ssh")}, nil
}

// maxIncludeDepth matches OpenSSH's limit on nested Include directives.
const maxIncludeDepth = 16

// Aliases returns the non-pattern Host aliases from the user's ssh_config,
// following Include directives the way OpenSSH does. Missing files are ignored.
func (c *Client) Aliases() ([]string, error) {
	seen := map[string]bool{}
	var out []string
	err := c.walkConfig(func(keyword string, args []string) {
		if !strings.EqualFold(keyword, "host") {
			return
		}
		for _, a := range args {
			if strings.ContainsAny(a, "*?!") || seen[a] {
				continue
			}
			seen[a] = true
			out = append(out, a)
		}
	})
	return out, err
}

// walkConfig calls visit for every directive in the user's ssh_config, in the
// order OpenSSH reads them. Include directives are followed in place: relative
// paths resolve against the ssh directory, "~" expands, globs expand in sorted
// order, missing files are ignored and nesting is limited to OpenSSH's depth.
// Include lines themselves are not passed to visit.
func (c *Client) walkConfig(visit func(keyword string, args []string)) error {
	sshDir := c.SSHDir
	if sshDir == "" {
		sshDir = filepath.Dir(c.SSHConfig)
	}
	return walkConfigFile(c.SSHConfig, sshDir, 0, visit)
}

func walkConfigFile(path, sshDir string, depth int, visit func(string, []string)) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%s: too many nested Include directives", path)
	}
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
		keyword, args := splitDirective(sc.Text())
		if keyword == "" {
			continue
		}
		if !strings.EqualFold(keyword, "include") {
			visit(keyword, args)
			continue
		}
		for _, a := range args {
			matches, err := filepath.Glob(expandIncludePath(a, sshDir))
			if err != nil {
				return fmt.Errorf("%s: Include %s: %w", path, a, err)
			}
			for _, m := range matches {
				if err := walkConfigFile(m, sshDir, depth+1, visit); err != nil {
					return err
				}
			}
		}
	}
	return sc.Err()
}

// splitDirective splits an ssh_config line into its keyword and arguments,
// accepting both "Keyword value" and "Keyword=value" forms.
func splitDirective(line string) (string, []string) {
	line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, nil
	}
	rest := strings.TrimLeft(line[i:], " \t")
	rest = strings.TrimPrefix(rest, "=")
	return line[:i], strings.Fields(rest)
}

// expandIncludePath resolves an Include argument: "~" is the user's home and
// relative paths are relative to the user's ~/.ssh directory.
func expandIncludePath(p, sshDir string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(sshDir, p)
}

// SafeSettings reports only non-secret, useful directives from the user
// config and the files it Includes, in the order OpenSSH reads them.
func (c *Client) SafeSettings() ([]string, error) {
	allowed := map[string]bool{"host": true, "hostname": true, "user": true, "port": true, "identityfile": true, "identitiesonly": true, "forwardagent": true, "addkeystoagent": true}
	var settings []string
	err := c.walkConfig(func(keyword string, args []string) {
		if len(args) == 0 || !allowed[strings.ToLower(keyword)] {
			return
		}
		settings = append(settings, keyword+" "+strings.Join(args, " "))
	})
	return settings, err
}

func (c *Client) Connect(ctx context.Context, alias string, key string, claude bool) error {
	argv := ConnectArgs(alias, key, claude)
	return runInteractive(ctx, argv[0], argv[1:]...)
}

// ConnectArgs returns the ssh command line for alias. A non-empty key is the
// Selected identity and is passed with -i; claude runs claude on the remote
// host, with -t because ssh gives a remote command no terminal otherwise.
func ConnectArgs(alias, key string, claude bool) []string {
	argv := []string{"ssh"}
	if claude {
		argv = append(argv, "-t")
	}
	if key != "" {
		argv = append(argv, "-i", key)
	}
	argv = append(argv, alias)
	if claude {
		argv = append(argv, "claude")
	}
	return argv
}
func runInteractive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func HostAliases(path string) ([]string, error) { c := &Client{SSHConfig: path}; return c.Aliases() }
func AgentStatus() (string, error) {
	out, err := exec.Command("ssh-add", "-l").CombinedOutput()
	if err != nil {
		message := strings.ToLower(string(out))
		if strings.Contains(message, "no identities") || strings.Contains(message, "no identities found") || strings.Contains(message, "has no identities") {
			return "running, no identities loaded", nil
		}
		return "unavailable or no accessible agent", nil
	}
	return "running; identities loaded", nil
}

// KeyLoaded compares the selected key's public fingerprint to ssh-add -l output.
// It never derives a public key from private material or performs a signing request.
func KeyLoaded(path string) (loaded, checkable bool, err error) {
	pub := path + ".pub"
	if _, err := os.Stat(pub); err != nil {
		return false, false, nil
	}
	fingerprint, err := exec.Command("ssh-keygen", "-lf", pub).CombinedOutput()
	if err != nil {
		return false, false, fmt.Errorf("ssh-keygen: %s: %w", strings.TrimSpace(string(fingerprint)), err)
	}
	fields := strings.Fields(string(fingerprint))
	if len(fields) < 2 {
		return false, false, fmt.Errorf("could not read public key fingerprint")
	}
	loadedKeys, err := exec.Command("ssh-add", "-l").CombinedOutput()
	if err != nil {
		return false, true, nil
	}
	return strings.Contains(string(loadedKeys), fields[1]), true, nil
}
func AddKey(path string) error { return runInteractive(context.Background(), "ssh-add", path) }
func KnownHostsPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".ssh", "known_hosts")
}
func RemoveKnownHost(host, path string) error {
	out, err := exec.Command("ssh-keygen", "-R", host, "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keygen: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
