package senzu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// flushRequest carries both the caller's context and the reply channel so the
// runLoop can propagate the context into the HTTP request, enabling cancellation.
type flushRequest struct {
	ctx   context.Context
	errCh chan error
}

// batcher manages buffered, batched delivery of telemetry events to the API.
// It uses a channel-based approach: producers call enqueue() which does a
// non-blocking send; a background goroutine drains the channel on a timer or
// when batchSize events accumulate.
type batcher struct {
	apiKey    string
	baseURL   string
	batchSize int
	interval  time.Duration
	debugMode bool
	disabled  bool

	ch         chan telemetryEvent
	flushCh    chan flushRequest // synchronous flush requests
	stopCh     chan struct{}
	wg         sync.WaitGroup
	httpClient *http.Client
}

func newBatcher(apiKey, baseURL string, batchSize, intervalMs int, debug, disabled bool) *batcher {
	b := &batcher{
		apiKey:     apiKey,
		baseURL:    baseURL,
		batchSize:  batchSize,
		interval:   time.Duration(intervalMs) * time.Millisecond,
		debugMode:  debug,
		disabled:   disabled,
		ch:         make(chan telemetryEvent, batchSize*10),
		flushCh:    make(chan flushRequest, 1),
		stopCh:     make(chan struct{}),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	if !disabled {
		b.wg.Add(1)
		go b.runLoop()
	}
	return b
}

// enqueue adds an event to the buffer. Non-blocking: if the channel is full
// the event is dropped (best-effort, same semantics as the TS SDK buffer).
func (b *batcher) enqueue(ev telemetryEvent) {
	if b.disabled {
		return
	}
	if b.debugMode {
		log.Printf("[senzu] %s", formatDebugEvent(ev))
	}
	select {
	case b.ch <- ev:
	default:
		// Channel full — drop to avoid blocking the caller.
	}
}

// flush drains all currently buffered events and sends them synchronously.
// Blocks until the POST completes or ctx is cancelled.
func (b *batcher) flush(ctx context.Context) error {
	if b.disabled {
		return nil
	}
	errCh := make(chan error, 1)
	req := flushRequest{ctx: ctx, errCh: errCh}
	select {
	case b.flushCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop signals the runLoop to drain and exit, then waits for it.
func (b *batcher) stop() {
	if b.disabled {
		return
	}
	close(b.stopCh)
	b.wg.Wait()
}

// runLoop is the background goroutine: drains the channel on a ticker or
// when batchSize is reached, and handles synchronous flush requests.
func (b *batcher) runLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	var pending []telemetryEvent

	for {
		select {
		case ev := <-b.ch:
			pending = append(pending, ev)
			if len(pending) >= b.batchSize {
				b.sendBackground(pending)
				pending = pending[:0]
			}

		case <-ticker.C:
			if len(pending) > 0 {
				b.sendBackground(pending)
				pending = pending[:0]
			}

		case req := <-b.flushCh:
			// Drain any events sitting in the channel first.
		drain:
			for {
				select {
				case ev := <-b.ch:
					pending = append(pending, ev)
				default:
					break drain
				}
			}
			var sendErr error
			if len(pending) > 0 {
				sendErr = b.sendWithContext(req.ctx, pending)
				pending = pending[:0]
			}
			req.errCh <- sendErr

		case <-b.stopCh:
			// Drain remaining events before exiting.
			close(b.ch)
			for ev := range b.ch {
				pending = append(pending, ev)
			}
			if len(pending) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				b.sendWithContext(ctx, pending) //nolint:errcheck
				cancel()
			}
			return
		}
	}
}

// sendBackground sends a batch without blocking the runLoop.
// On error it logs and re-enqueues best-effort.
func (b *batcher) sendBackground(events []telemetryEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.sendWithContext(ctx, events); err != nil {
		log.Printf("[senzu:sdk] flush error: %v", err)
		for i := len(events) - 1; i >= 0; i-- {
			select {
			case b.ch <- events[i]:
			default:
			}
		}
	}
}

func (b *batcher) sendWithContext(ctx context.Context, events []telemetryEvent) error {
	payload := map[string]any{"events": events}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("senzu: marshal events: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.baseURL+"/api/v1/events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("senzu: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("senzu: send events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("senzu: server returned %d", resp.StatusCode)
	}
	return nil
}

// bufferedCount returns the number of events currently in the channel.
// Used by tests only.
func (b *batcher) bufferedCount() int {
	return len(b.ch)
}
