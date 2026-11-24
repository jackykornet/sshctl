package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// shellTree builds sshctl into a directory put first on PATH, over a home
// whose ssh_config has web01 and web02, for the completion scripts to run.
func shellTree(t *testing.T) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds sshctl")
	}
	home := fakeHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host web01\nHost web02\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	exe := filepath.Join(bin, "sshctl")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The bash script completes through sshctl, as CI's Ubuntu bash runs it.
func TestBashCompletionScript(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("no bash")
	}
	env := shellTree(t)
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"sshctl boo", []string{"bootstrap"}},
		{"sshctl bootstrap w", []string{"web01", "web02"}},
		{"sshctl list --space=", []string{"default"}},
		{"sshctl completion ", []string{"bash", "zsh", "fish", "powershell"}},
	} {
		// COMP_WORDS as bash splits the line, at spaces and =.
		script := `eval "$(sshctl completion bash)"
COMP_LINE=$1 COMP_POINT=${#1}
read -ra COMP_WORDS <<<"${1//=/ = }"
[[ $1 == *' ' || $1 == *= ]] && COMP_WORDS+=("")
COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
_sshctl
printf '%s\n' "${COMPREPLY[@]}"`
		cmd := exec.Command(bash, "-c", script, "bash", tc.line)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.line, err, out)
		}
		if got := strings.Fields(string(out)); !slices.Equal(got, tc.want) {
			t.Errorf("bash %q<TAB> = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// The PowerShell script completes through sshctl, as CI's Windows
// PowerShell runs it.
func TestPowerShellCompletionScript(t *testing.T) {
	ps, err := exec.LookPath("pwsh")
	if err != nil {
		if ps, err = exec.LookPath("powershell"); err != nil {
			t.Skip("no PowerShell")
		}
	}
	env := shellTree(t)
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"sshctl boo", []string{"bootstrap"}},
		{"sshctl bootstrap w", []string{"web01", "web02"}},
		{"sshctl bootstrap ", []string{"web01", "web02"}},
		{"sshctl completion p", []string{"powershell"}},
	} {
		// -Command joins the words after it into the command, so the line
		// goes in the environment.
		script := `sshctl completion powershell | Out-String | Invoke-Expression
$line = $env:SSHCTL_TEST_LINE
(TabExpansion2 -inputScript $line -cursorColumn $line.Length).CompletionMatches | ForEach-Object { $_.CompletionText }`
		cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script)
		cmd.Env = append(env, "SSHCTL_TEST_LINE="+tc.line)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.line, err, out)
		}
		if got := strings.Fields(string(out)); !slices.Equal(got, tc.want) {
			t.Errorf("PowerShell %q<TAB> = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// The zsh and fish scripts at least parse, where those shells are.
func TestZshAndFishCompletionScriptsParse(t *testing.T) {
	for _, shell := range []string{"zsh", "fish"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		a := completeTree(t)
		if err := a.execute([]string{"completion", shell}); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(path, "-n")
		cmd.Stdin = strings.NewReader(output(a))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s -n: %v\n%s", shell, err, out)
		}
	}
}
