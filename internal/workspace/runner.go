package workspace

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	pty "github.com/aymanbagabas/go-pty"
)

// runner is the live process behind a Session: a command in a pseudo-terminal
// whose output is parsed by a vterm.
type runner struct {
	pty  pty.Pty
	cmd  *pty.Cmd
	term *vterm
	// scroll is how many lines the pane is scrolled back into history.
	scroll int
	// activity is set when the program writes output (a bell is output too)
	// and cleared when the Workspace takes it.
	activity  atomic.Bool
	closeOnce sync.Once
	ptyOnce   sync.Once
}

// closePty releases the pseudo-terminal. go-pty's Close is not safe to call concurrently.
func (r *runner) closePty() { r.ptyOnce.Do(func() { r.pty.Close() }) }

// Messages a runner sends to the Workspace.
type (
	redrawMsg       struct{}
	sessionEndedMsg struct {
		sess *Session
		r    *runner
		code int
	}
	clipboardMsg struct{ text string }
)

// events carries runner messages to the Bubble Tea loop. redraw is separate
// and holds at most one pending notification, so bursts of output coalesce.
type events struct {
	ch     chan tea.Msg
	redraw chan struct{}
}

func newEvents() *events {
	return &events{ch: make(chan tea.Msg, 16), redraw: make(chan struct{}, 1)}
}

func (e *events) notifyRedraw() {
	select {
	case e.redraw <- struct{}{}:
	default:
	}
}

// listen waits for the next runner message.
func (e *events) listen() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-e.ch:
			return msg
		case <-e.redraw:
			return redrawMsg{}
		}
	}
}

// startRunner starts argv in a w×h pseudo-terminal for sess.
func startRunner(argv []string, w, h, scrollback int, sess *Session, ev *events) (*runner, error) {
	if len(argv) == 0 {
		return nil, errors.New("no command to run")
	}
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	if err := p.Resize(w, h); err != nil {
		p.Close()
		return nil, err
	}
	cmd := p.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if err := cmd.Start(); err != nil {
		p.Close()
		return nil, err
	}
	r := &runner{pty: p, cmd: cmd}
	r.term = newVterm(w, h, scrollback, func(text string) { ev.ch <- clipboardMsg{text} })

	output := make(chan struct{})
	var lastRead atomic.Int64
	go func() { // program output -> emulator
		defer close(output)
		buf := make([]byte, 32*1024)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				lastRead.Store(time.Now().UnixNano())
				r.term.Write(buf[:n])
				r.activity.Store(true)
				ev.notifyRedraw()
			}
			if err != nil {
				return
			}
		}
	}()
	go io.Copy(p, r.term) // keys, mouse and terminal replies -> program
	go func() {
		err := cmd.Wait()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		} else if err != nil {
			code = -1
		}
		// go-pty keeps the child's side of the terminal open, so reads never
		// hit EOF on their own. Let buffered output drain, then close.
		// The quiet period starts at exit too, so a process that fails at once
		// (such as ssh printing "Connection refused") still shows its message.
		exited := time.Now()
		deadline := exited.Add(time.Second)
		for time.Now().Before(deadline) {
			last := time.Unix(0, lastRead.Load())
			if last.Before(exited) {
				last = exited
			}
			if time.Since(last) >= 100*time.Millisecond {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		r.closePty()
		<-output
		ev.ch <- sessionEndedMsg{sess: sess, r: r, code: code}
	}()
	return r, nil
}

func (r *runner) resize(w, h int) {
	if cw, ch := r.term.Size(); cw == w && ch == h {
		return
	}
	r.pty.Resize(w, h)
	r.term.Resize(w, h)
}

// close ends the process and releases the pseudo-terminal. It is safe to call more than once.
func (r *runner) close() {
	r.closeOnce.Do(func() {
		if r.cmd.Process != nil {
			r.cmd.Process.Kill()
		}
		r.closePty()
		r.term.Close()
	})
}
