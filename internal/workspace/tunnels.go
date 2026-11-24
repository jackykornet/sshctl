package workspace

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// A tunnelRun is the background process that carries one Connection's
// Tunnels (ADR-0012). It is not a Session: it has no tab, no pseudo-terminal,
// and does not make its Connection open or change History.
type tunnelRun struct {
	alias string
	// space is the Connection's Space when the tunnel started, for a
	// Connection that has since left the list.
	space string
	// forwards are the Tunnels the process was started with.
	forwards []Tunnel
	// err is set when the process exited without being stopped: ssh's
	// error, which the Tunnels section shows after ✕.
	err  string
	proc *tunnelProc
}

// failed reports whether the process exited on its own.
func (r *tunnelRun) failed() bool { return r.err != "" }

// tunnelProc is a tunnel's ssh process. Its stderr is kept for the error
// shown when it exits.
type tunnelProc struct {
	cmd    *exec.Cmd
	stderr *tailBuffer
	// exited is closed once the process has exited.
	exited chan struct{}
}

// tunnelEndedMsg says a tunnel's process exited.
type tunnelEndedMsg struct {
	run    *tunnelRun
	proc   *tunnelProc
	code   int
	stderr string
}

// tunnels owns every running tunnel: one process per Connection, in the
// order they started, whatever Space the Connection is in.
type tunnels struct {
	ev *events
	// command returns the argv for alias's tunnel; nil starts no process
	// (layout tests).
	command func(alias string, l Launch) ([]string, error)
	runs    []*tunnelRun
}

// get returns alias's tunnel, running or failed, or nil.
func (t *tunnels) get(alias string) *tunnelRun {
	i := slices.IndexFunc(t.runs, func(r *tunnelRun) bool { return r.alias == alias })
	if i < 0 {
		return nil
	}
	return t.runs[i]
}

// running reports whether alias's tunnel is running (listed and not failed).
func (t *tunnels) running(alias string) bool {
	r := t.get(alias)
	return r != nil && !r.failed()
}

// count is how many Tunnels (forwards) are running.
func (t *tunnels) count() int {
	n := 0
	for _, r := range t.runs {
		if !r.failed() {
			n += len(r.forwards)
		}
	}
	return n
}

// start runs c's Tunnels in the background, replacing any process its
// tunnel had. A tunnel that cannot start is not listed.
func (t *tunnels) start(c *Connection) error {
	switch {
	case c.Orphaned:
		return fmt.Errorf("%s: %w", c.Alias, ErrOrphaned)
	case len(c.Tunnels) == 0:
		return fmt.Errorf("%s has no Tunnels", c.Alias)
	}
	var proc *tunnelProc
	if t.command != nil {
		argv, err := t.command(c.Alias, Launch{Tunnel: true})
		if err != nil {
			return fmt.Errorf("%s: %w", c.Alias, err)
		}
		if proc, err = startTunnelProc(argv); err != nil {
			return fmt.Errorf("%s: %w", c.Alias, err)
		}
	}
	r := t.get(c.Alias)
	if r == nil {
		r = &tunnelRun{alias: c.Alias}
		t.runs = append(t.runs, r)
	} else {
		r.proc.kill()
	}
	r.space, r.forwards, r.err, r.proc = c.Space, slices.Clone(c.Tunnels), "", proc
	if proc != nil {
		go t.wait(r, proc)
	}
	return nil
}

// stop ends alias's tunnel and drops it from the list.
func (t *tunnels) stop(alias string) {
	t.runs = slices.DeleteFunc(t.runs, func(r *tunnelRun) bool {
		if r.alias == alias {
			r.proc.kill()
			return true
		}
		return false
	})
}

// stopAll ends every tunnel.
func (t *tunnels) stopAll() {
	for _, r := range t.runs {
		r.proc.kill()
	}
	t.runs = nil
}

// ended records that a tunnel's process exited. It fails the tunnel only
// when proc is still its process, so a process replaced by a restart or
// ended by stop is ignored, and returns the failed tunnel.
func (t *tunnels) ended(msg tunnelEndedMsg) *tunnelRun {
	if !slices.Contains(t.runs, msg.run) || msg.run.proc != msg.proc {
		return nil
	}
	msg.run.err = tunnelError(msg.stderr, msg.code)
	return msg.run
}

// batchHint is added to the error of a tunnel that BatchMode stopped from
// asking for a password, passphrase or host key confirmation.
const batchHint = "needs a password or host-key confirmation: connect once in a tab, or Bootstrap"

// tunnelError is the error a tunnel shows for ssh's stderr and exit code:
// ssh's last line, with batchHint when ssh wanted to prompt.
func tunnelError(stderr string, code int) string {
	msg := ""
	for _, l := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			msg = l
		}
	}
	if msg == "" {
		msg = fmt.Sprintf("ssh exited (exit %d)", code)
	}
	lower := strings.ToLower(stderr)
	for _, s := range []string{"permission denied", "host key verification failed", "passphrase", "password"} {
		if strings.Contains(lower, s) {
			return msg + " (" + batchHint + ")"
		}
	}
	return msg
}

// wait sends tunnelEndedMsg when proc exits.
func (t *tunnels) wait(r *tunnelRun, proc *tunnelProc) {
	err := proc.cmd.Wait()
	close(proc.exited)
	code := -1
	if proc.cmd.ProcessState != nil {
		code = proc.cmd.ProcessState.ExitCode()
	} else if err == nil {
		code = 0
	}
	t.ev.ch <- tunnelEndedMsg{run: r, proc: proc, code: code, stderr: proc.stderr.String()}
}

// startTunnelProc starts argv with no terminal: no stdin, output dropped,
// stderr kept.
func startTunnelProc(argv []string) (*tunnelProc, error) {
	if len(argv) == 0 {
		return nil, errors.New("no command to run")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	p := &tunnelProc{cmd: cmd, stderr: &tailBuffer{}, exited: make(chan struct{})}
	cmd.Stderr = p.stderr
	// A killed ssh's children could hold stderr open; don't wait for them.
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return p, nil
}

// kill ends the process; nil (no process) is fine.
func (p *tunnelProc) kill() {
	if p != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}

// tailBufferSize is how much of a tunnel's stderr is kept.
const tailBufferSize = 4096

// tailBuffer keeps the last tailBufferSize bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - tailBufferSize; over > 0 {
		b.buf = slices.Clone(b.buf[over:])
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// toggleTunnel stops alias's tunnel while it runs, and otherwise starts it,
// restarting a failed one. A Connection with no Tunnels opens its edit form
// on the Tunnels field instead.
func (m *Model) toggleTunnel(alias string) tea.Cmd {
	if m.tunnels.running(alias) {
		return m.stopTunnel(alias)
	}
	if c := m.st.Get(alias); c != nil && len(c.Tunnels) == 0 && !c.Orphaned {
		m.tunnels.stop(alias) // a failed tunnel whose Tunnels were all removed
		m.fixTunnelCursor()
		return m.startEditAt(fieldTunnels)
	}
	return m.startTunnel(alias)
}

// startTunnel starts, or restarts, alias's Tunnels as they are saved now.
func (m *Model) startTunnel(alias string) tea.Cmd {
	c := m.st.Get(alias)
	if c == nil {
		m.tunnels.stop(alias)
		m.fixTunnelCursor()
		return m.setStatus(alias+" is no longer in the connection list", true)
	}
	if len(c.Tunnels) == 0 {
		m.tunnels.stop(alias)
		m.fixTunnelCursor()
		return m.setStatus(alias+" has no Tunnels", true)
	}
	if err := m.tunnels.start(c); err != nil {
		if errors.Is(err, ErrOrphaned) {
			return m.setStatus(alias+" is missing from ssh_config", true)
		}
		return m.setStatus(err.Error(), true)
	}
	return m.setStatus(fmt.Sprintf("%s: started %s", alias, countTunnels(len(c.Tunnels))), false)
}

// stopTunnel stops alias's tunnel.
func (m *Model) stopTunnel(alias string) tea.Cmd {
	m.tunnels.stop(alias)
	m.fixTunnelCursor()
	return m.setStatus(alias+": tunnel stopped", false)
}

func countTunnels(n int) string {
	if n == 1 {
		return "1 tunnel"
	}
	return fmt.Sprintf("%d tunnels", n)
}

// fixTunnelCursor keeps the cursor on a row of the Tunnels section, or puts
// it back on the list once the section has no rows.
func (m *Model) fixTunnelCursor() {
	if m.tunnelCursor < 0 {
		return
	}
	n := len(m.tunnelRows())
	if n == 0 {
		m.tunnelCursor = -1
		return
	}
	m.tunnelCursor = min(m.tunnelCursor, n-1)
}

// tunnelRow is one row of the Tunnels section: one forward of a tunnel.
type tunnelRow struct {
	run     *tunnelRun
	forward Tunnel
}

// tunnelRows are the Tunnels section's rows, one per forward of every
// tunnel, in the order the tunnels started.
func (m *Model) tunnelRows() []tunnelRow {
	var rows []tunnelRow
	for _, r := range m.tunnels.runs {
		for _, f := range r.forwards {
			rows = append(rows, tunnelRow{r, f})
		}
	}
	return rows
}

// tunnelTarget is the alias of the tunnel row under the cursor, when the
// cursor is in the Tunnels section.
func (m *Model) tunnelTarget() (string, bool) {
	rows := m.tunnelRows()
	if m.focus != focusList || m.tunnelCursor < 0 || m.tunnelCursor >= len(rows) {
		return "", false
	}
	return rows[m.tunnelCursor].run.alias, true
}

// moveCursor moves the list cursor by delta rows: down from the list's last
// row into the Tunnels section, and up from its first row back to the list.
func (m *Model) moveCursor(delta int) {
	n := len(m.tunnelRows())
	if m.tunnelCursor >= 0 {
		if m.tunnelCursor+delta < 0 {
			m.tunnelCursor = -1 // the list cursor is still on its last row
			return
		}
		m.tunnelCursor = min(m.tunnelCursor+delta, n-1)
		return
	}
	rows := m.st.Rows()
	if delta > 0 && n > 0 && m.st.CursorIndex(rows) >= len(rows)-1 {
		m.tunnelCursor = 0
		return
	}
	m.st.MoveCursor(delta)
}
