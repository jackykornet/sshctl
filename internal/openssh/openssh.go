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
func (c *Client) Aliases() ([]string, error) {
	f, err := os.Open(c.SSHConfig)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.SplitN(sc.Text(), "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "host") {
			continue
		}
		for _, a := range fields[1:] {
			if strings.ContainsAny(a, "*?!") || seen[a] {
				continue
			}
			seen[a] = true
			out = append(out, a)
		}
	}
	return out, sc.Err()
}

// SafeSettings reports only non-secret, useful directives from the user config.
func (c *Client) SafeSettings() ([]string, error) {
	f, err := os.Open(c.SSHConfig)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	allowed := map[string]bool{"host": true, "hostname": true, "user": true, "port": true, "identityfile": true, "identitiesonly": true, "forwardagent": true, "addkeystoagent": true}
	var settings []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.SplitN(sc.Text(), "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 || !allowed[strings.ToLower(fields[0])] {
			continue
		}
		settings = append(settings, fields[0]+" "+strings.Join(fields[1:], " "))
	}
	return settings, sc.Err()
}

func (c *Client) Connect(ctx context.Context, alias string, key string, claude bool) error {
	args := []string{}
	if key != "" {
		args = append(args, "-i", key)
	}
	args = append(args, alias)
	if claude {
		args = append(args, "claude")
	}
	return runInteractive(ctx, "ssh", args...)
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
