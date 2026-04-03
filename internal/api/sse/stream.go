// Package sse provides Server-Sent Events streaming for deployment logs
// and dashboard-level real-time events.
// Topics use string keys: "log:{jobID}" for per-job logs, "dashboard" for job status events.
package sse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	// TopicDashboard is the topic for dashboard-level events (job status changes).
	TopicDashboard = "dashboard"
)

// LogTopic returns the SSE topic for a job's log stream.
func LogTopic(jobID string) string {
	return "log:" + jobID
}

// Broadcaster manages topic-based SSE subscriptions.
// Topics can be job log streams ("log:{jobID}") or dashboard events ("dashboard").
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[string][]chan string // topic → list of subscriber channels
	log  *slog.Logger
}

// NewBroadcaster creates a Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[string][]chan string),
		log:  slog.Default().With("component", "sse"),
	}
}

// Publish sends a message to all subscribers of the given topic.
func (b *Broadcaster) Publish(topic, line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[topic] {
		select {
		case ch <- line:
		default: // drop if subscriber is slow
		}
	}
}

// PublishJSON marshals data as JSON and publishes it to the given topic.
func (b *Broadcaster) PublishJSON(topic string, data any) {
	encoded, err := json.Marshal(data)
	if err != nil {
		b.log.Warn("failed to marshal SSE event", "topic", topic, "err", err)
		return
	}
	b.Publish(topic, string(encoded))
}

// Subscribe registers interest in messages for a topic.
// Returns the channel and a cleanup function the caller must invoke when done.
func (b *Broadcaster) Subscribe(topic string) (<-chan string, func()) {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[topic]
		for i, s := range subs {
			if s == ch {
				b.subs[topic] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(b.subs[topic]) == 0 {
			delete(b.subs, topic)
		}
		close(ch)
	}
	return ch, unsub
}

// setSSEHeaders sets the required SSE headers on a Fiber response.
func setSSEHeaders(c *fiber.Ctx) {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")
	// Disable compression for SSE — compressed chunks break EventSource parsing.
	c.Set("Content-Encoding", "identity")
}

// StreamLog streams a job's log file to the HTTP response using SSE.
// Uses the "log:{jobID}" topic for live events.
//
// fasthttp does not provide context.Context inside SetBodyStreamWriter.
// Client disconnect is detected via write errors (broken pipe).
func (b *Broadcaster) StreamLog(c *fiber.Ctx, jobID, logPath string) error {
	setSSEHeaders(c)

	// Subscribe to live log events.
	topic := LogTopic(jobID)
	ch, unsub := b.Subscribe(topic)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer unsub()

		// Re-play existing log file content first.
		if err := replayLog(w, logPath); err != nil {
			b.log.Warn("could not replay log", "job_id", jobID, "path", logPath, "err", err)
		}
		if err := w.Flush(); err != nil {
			return // client disconnected
		}

		streamFromChannelBufio(w, ch)
	})
	return nil
}

// StreamDashboard streams dashboard events to the HTTP response using SSE.
// Sends an initial snapshot of active jobs, then live status change events.
func (b *Broadcaster) StreamDashboard(c *fiber.Ctx, initialData string) error {
	setSSEHeaders(c)

	ch, unsub := b.Subscribe(TopicDashboard)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer unsub()

		// Send initial snapshot.
		if initialData != "" {
			writeSSEEvent(w, "init", initialData)
			if err := w.Flush(); err != nil {
				return // client disconnected
			}
		}

		streamFromChannelBufio(w, ch)
	})
	return nil
}

// streamFromChannelBufio reads from ch and writes SSE data lines.
// Exits when ch is closed or a write error occurs (client disconnect).
func streamFromChannelBufio(w *bufio.Writer, ch <-chan string) {
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			writeSSELine(w, line)
			if err := w.Flush(); err != nil {
				return // client disconnected
			}
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n") //nolint:errcheck
			if err := w.Flush(); err != nil {
				return // client disconnected
			}
		}
	}
}

// replayLog sends all existing lines from logPath as SSE data events.
func replayLog(w *bufio.Writer, logPath string) error {
	if logPath == "" {
		return nil
	}
	f, err := os.Open(logPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		writeSSELine(w, sc.Text())
	}
	return sc.Err()
}

func writeSSELine(w *bufio.Writer, line string) {
	fmt.Fprintf(w, "data: %s\n\n", line) //nolint:errcheck
}

func writeSSEEvent(w *bufio.Writer, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data) //nolint:errcheck
}
