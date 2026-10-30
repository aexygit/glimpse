// Package capture feeds game frames, live or from a recording, through protocol.Decode.
package capture

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nuriland/a2kit"
	"github.com/nuriland/a2kit/wire"

	"github.com/aexygit/glimpse/internal/protocol"
)

type Status string

const (
	StatusNeedsAdmin  Status = "needs_admin"
	StatusWaitingGame Status = "waiting_game"
	StatusCapturing   Status = "capturing"
	StatusError       Status = "error"
)

// Timers and seams, vars so tests can replace them.
var (
	retryAdmin = 10 * time.Second
	retryError = 5 * time.Second
	gameIdle   = 30 * time.Second

	decode   = protocol.Decode
	openLive = func(ctx context.Context, adapter string, cfg a2kit.Config) (source, error) {
		return a2kit.Capture(ctx, adapter, cfg)
	}
)

type source interface {
	Messages() iter.Seq2[a2kit.Message, error]
	Close() error
}

type Capturer struct {
	OnStatus  func(Status)
	OnMessage func(any)
}

// Run captures on every adapter until ctx is done, then returns nil.
// a2kit's decoder drops its lock after 60 s of silence or when the game
// connection closes, and locks again on the next one, so a game restart just
// shows up as frames stopping (waiting_game) and starting again (capturing).
func (c *Capturer) Run(ctx context.Context) error {
	st := &statusSetter{fn: c.OnStatus}
	for ctx.Err() == nil {
		wait := retryError
		r, err := openLive(ctx, "", a2kit.Config{})
		switch {
		case err == nil:
			st.set(StatusWaitingGame)
			err = c.pump(ctx, r, st)
			r.Close()
			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				err = errors.New("capture ended")
			}
			st.set(StatusError)
		case needsAdmin(err):
			st.set(StatusNeedsAdmin)
			wait = retryAdmin
		default:
			st.set(StatusError)
		}
		slog.Default().Warn("capture stopped, retrying", "err", err, "in", wait)

		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return nil
}

// pump reads r until it ends. A clean end returns nil.
func (c *Capturer) pump(ctx context.Context, r source, st *statusSetter) error {
	gen := st.newGen()
	idle := time.AfterFunc(gameIdle, func() {
		defer func() {
			if p := recover(); p != nil {
				slog.Default().Error("status callback panicked", "panic", p)
			}
		}()
		st.setGen(gen, StatusWaitingGame)
	})
	defer func() {
		idle.Stop()
		st.newGen() // a callback already fired must not overwrite what comes next
	}()
	return c.consume(ctx, r, func() {
		idle.Reset(gameIdle)
		st.set(StatusCapturing)
	})
}

// consume feeds r's server frames through the pipeline until r ends or ctx is done. A panic inside r's
// iterator (a2kit does not recover) is returned as an error.
func (c *Capturer) consume(ctx context.Context, r source, onFrame func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.Default().Error("frame source panicked", "panic", p)
			err = fmt.Errorf("frame source panicked: %v", p)
		}
	}()
	for m, err := range r.Messages() {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.Flags&wire.FromClient != 0 {
			continue
		}
		onFrame()
		c.handle(m.Frame)
	}
	return nil
}

// Replay runs a recording through the same pipeline.
func Replay(ctx context.Context, path string, onMessage func(any)) error {
	r, err := a2kit.Open(path, a2kit.Config{})
	if err != nil {
		return err
	}
	defer r.Close()
	return (&Capturer{OnMessage: onMessage}).consume(ctx, r, func() {})
}

func (c *Capturer) handle(f wire.Frame) {
	defer func() {
		if p := recover(); p != nil {
			slog.Default().Error("frame handler panicked", "panic", p, "opcode", f.Opcode.String())
		}
	}()
	res, err := decode(f)
	if err != nil {
		slog.Default().Debug("decode failed", "err", err, "opcode", f.Opcode.String())
		return
	}
	if res != nil && c.OnMessage != nil {
		c.OnMessage(res)
	}
}

// statusSetter calls fn on changes only. It is shared with the idle timer.
type statusSetter struct {
	mu  sync.Mutex
	cur Status
	gen int
	fn  func(Status)
}

func (s *statusSetter) newGen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	return s.gen
}

// setGen is set, unless a newer generation started since gen was taken.
func (s *statusSetter) setGen(gen int, v Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen == s.gen {
		s.setLocked(v)
	}
}

func (s *statusSetter) set(v Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setLocked(v)
}

func (s *statusSetter) setLocked(v Status) {
	if v == s.cur {
		return
	}
	s.cur = v
	if s.fn != nil {
		s.fn(v)
	}
}

func needsAdmin(err error) bool {
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"administrator", "permission denied", "operation not permitted", "access is denied"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
