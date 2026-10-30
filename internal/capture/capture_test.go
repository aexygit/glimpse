package capture

import (
	"context"
	"errors"
	"io/fs"
	"iter"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nuriland/a2kit"
	"github.com/nuriland/a2kit/wire"
)

// frames in testdata/sample.pcap, per sample.expect.json.
const sampleFrames = 4

func TestReplayPipeline(t *testing.T) {
	var n int
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { n++; return n, nil }))

	var got []any
	if err := Replay(t.Context(), "testdata/sample.pcap", func(m any) { got = append(got, m) }); err != nil {
		t.Fatal(err)
	}
	if n != sampleFrames || len(got) != sampleFrames {
		t.Errorf("decoded %d, delivered %d, want %d", n, len(got), sampleFrames)
	}
}

func TestReplayMissingFile(t *testing.T) {
	if err := Replay(t.Context(), "testdata/nope.pcap", func(any) {}); err == nil {
		t.Fatal("want error")
	}
}

func TestReplayCancelled(t *testing.T) {
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { return 1, nil }))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Replay(ctx, "testdata/sample.pcap", func(any) { t.Error("delivered after cancel") })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDecodePanicRecovered(t *testing.T) {
	var n int
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) {
		n++
		if n == 1 {
			panic("boom")
		}
		return n, nil
	}))

	var got []any
	if err := Replay(t.Context(), "testdata/sample.pcap", func(m any) { got = append(got, m) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != sampleFrames-1 {
		t.Errorf("delivered %d, want %d", len(got), sampleFrames-1)
	}
}

func TestDecodeNilNotDelivered(t *testing.T) {
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { return nil, nil }))
	if err := Replay(t.Context(), "testdata/sample.pcap", func(any) { t.Error("delivered nil") }); err != nil {
		t.Fatal(err)
	}
}

type fakeSource struct {
	msgs []a2kit.Message
	err  error
	// block holds the iteration open until ctx is done.
	block context.Context
	// panicVal, if set, is panicked with after msgs.
	panicVal any
}

func (f fakeSource) Messages() iter.Seq2[a2kit.Message, error] {
	return func(yield func(a2kit.Message, error) bool) {
		for _, m := range f.msgs {
			if !yield(m, nil) {
				return
			}
		}
		if f.panicVal != nil {
			panic(f.panicVal)
		}
		if f.block != nil {
			<-f.block.Done()
			return
		}
		if f.err != nil {
			yield(a2kit.Message{}, f.err)
		}
	}
}

func (fakeSource) Close() error { return nil }

func serverMsg() a2kit.Message {
	return a2kit.Message{Frame: wire.Frame{Flags: wire.FromServer}}
}

func fastTimers(t *testing.T) {
	t.Helper()
	a, b, c := retryAdmin, retryError, gameIdle
	retryAdmin, retryError, gameIdle = 5*time.Millisecond, 5*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { retryAdmin, retryError, gameIdle = a, b, c })
}

type statusLog struct {
	mu sync.Mutex
	s  []Status
	ch chan struct{}
}

func newStatusLog() *statusLog { return &statusLog{ch: make(chan struct{}, 64)} }

func (l *statusLog) add(s Status) {
	l.mu.Lock()
	l.s = append(l.s, s)
	l.mu.Unlock()
	l.ch <- struct{}{}
}

func (l *statusLog) waitFor(t *testing.T, n int) []Status {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		l.mu.Lock()
		got := slices.Clone(l.s)
		l.mu.Unlock()
		if len(got) >= n {
			return got
		}
		select {
		case <-l.ch:
		case <-timeout:
			t.Fatalf("timeout, statuses so far: %v", got)
		}
	}
}

func runCapturer(t *testing.T, open func(int, context.Context) (source, error), c *Capturer) (stop func()) {
	t.Helper()
	var calls int
	var mu sync.Mutex
	orig := openLive
	openLive = func(ctx context.Context, _ string, _ a2kit.Config) (source, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		return open(n, ctx)
	}
	t.Cleanup(func() { openLive = orig })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("Run returned %v after cancel", err)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestRunStatusCycle(t *testing.T) {
	fastTimers(t)
	log := newStatusLog()
	var msgs []any
	var mu sync.Mutex
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { return 1, nil }))

	c := &Capturer{
		OnStatus:  log.add,
		OnMessage: func(m any) { mu.Lock(); msgs = append(msgs, m); mu.Unlock() },
	}
	stop := runCapturer(t, func(n int, ctx context.Context) (source, error) {
		switch n {
		case 1:
			return nil, fs.ErrPermission
		case 2:
			return fakeSource{msgs: []a2kit.Message{serverMsg(), serverMsg()}, err: errors.New("link down")}, nil
		default:
			return fakeSource{block: ctx}, nil
		}
	}, c)

	got := log.waitFor(t, 5)
	stop()
	want := []Status{StatusNeedsAdmin, StatusWaitingGame, StatusCapturing, StatusError, StatusWaitingGame}
	if !slices.Equal(got[:5], want) {
		t.Errorf("statuses %v, want prefix %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 2 {
		t.Errorf("delivered %d messages, want 2", len(msgs))
	}
}

func TestRunClientFramesIgnored(t *testing.T) {
	fastTimers(t)
	log := newStatusLog()
	var n int
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { n++; return 1, nil }))
	stop := runCapturer(t, func(_ int, ctx context.Context) (source, error) {
		return fakeSource{msgs: []a2kit.Message{{Frame: wire.Frame{Flags: wire.FromClient}}}, block: ctx}, nil
	}, &Capturer{OnStatus: log.add})

	log.waitFor(t, 1)
	stop()
	if n != 0 || slices.Contains(log.waitFor(t, 1), StatusCapturing) {
		t.Errorf("client frame counted: decoded %d, statuses %v", n, log.waitFor(t, 1))
	}
}

func TestRunSourcePanicReopens(t *testing.T) {
	fastTimers(t)
	log := newStatusLog()
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { return nil, nil }))
	stop := runCapturer(t, func(n int, ctx context.Context) (source, error) {
		if n == 1 {
			return fakeSource{msgs: []a2kit.Message{serverMsg()}, panicVal: "parse boom"}, nil
		}
		return fakeSource{block: ctx}, nil
	}, &Capturer{OnStatus: log.add})

	got := log.waitFor(t, 4)
	stop()
	want := []Status{StatusWaitingGame, StatusCapturing, StatusError, StatusWaitingGame}
	if !slices.Equal(got[:4], want) {
		t.Errorf("statuses %v, want prefix %v", got, want)
	}
}

func TestReplaySourcePanic(t *testing.T) {
	err := (&Capturer{}).consume(t.Context(), fakeSource{panicVal: "boom"}, func() {})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRunBackToWaitingWhenGameGoesQuiet(t *testing.T) {
	fastTimers(t)
	log := newStatusLog()
	t.Cleanup(swapDecode(func(wire.Frame) (any, error) { return nil, nil }))
	stop := runCapturer(t, func(_ int, ctx context.Context) (source, error) {
		return fakeSource{msgs: []a2kit.Message{serverMsg()}, block: ctx}, nil
	}, &Capturer{OnStatus: log.add})

	got := log.waitFor(t, 3)
	stop()
	want := []Status{StatusWaitingGame, StatusCapturing, StatusWaitingGame}
	if !slices.Equal(got[:3], want) {
		t.Errorf("statuses %v, want %v", got, want)
	}
}

func swapDecode(f func(wire.Frame) (any, error)) (restore func()) {
	orig := decode
	decode = f
	return func() { decode = orig }
}
