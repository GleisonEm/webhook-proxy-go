package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LokiCore is a custom zapcore that pushes logs to Loki
type LokiCore struct {
	zapcore.LevelEnabler
	encoder zapcore.Encoder
	url     string
	appName string
	client  *http.Client

	// Batching fields
	entryChan  chan lokiEntry
	batchSize  int
	batchWait  time.Duration
	wg         sync.WaitGroup
	shutdownCh chan struct{}
}

type lokiEntry struct {
	Timestamp string
	Line      string
	Level     string
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

func NewLokiCore(level zapcore.Level, url, appName string) *LokiCore {
	c := &LokiCore{
		LevelEnabler: level,
		encoder:      zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		url:          url + "/loki/api/v1/push",
		appName:      appName,
		client:       &http.Client{Timeout: 5 * time.Second},
		entryChan:    make(chan lokiEntry, 1000), // Buffer up to 1000 entries
		batchSize:    100,                        // Flush every 100 entries
		batchWait:    2 * time.Second,            // Or every 2 seconds
		shutdownCh:   make(chan struct{}),
	}

	c.wg.Add(1)
	go c.run()

	return c
}

func (c *LokiCore) With(fields []zapcore.Field) zapcore.Core {
	// For With(), we typically create a clone.
	// However, cloning a batched core is tricky because multiple cores would share the same channel or need new ones.
	// To simplify for this specific use case (app_name is usually static), we share the channel.
	// BUT `zap` expects `With` to return a new Core.
	// If we share the channel, it's fine as long as the worker matches.
	// The problem is `Sync` and lifecycle management.
	// A simpler approach for `With` in a singleton-like usage is to return self or a shallow copy sharing the channel.
	// Given the previous implementation returned a new pointer, let's do safe shallow copy sharing the channel.

	clone := &LokiCore{
		LevelEnabler: c.LevelEnabler,
		encoder:      c.encoder.Clone(),
		url:          c.url,
		appName:      c.appName,
		client:       c.client,
		entryChan:    c.entryChan, // Share the channel
		batchSize:    c.batchSize,
		batchWait:    c.batchWait,
		// We DO NOT add to wg here because the worker is owned by the original creator.
		// This suggests that NewLokiCore starts the manager.
		// Caveat: If the original core is discarded but clones exist, who stops the worker?
		// In this app, Logger is global/singleton, so it's acceptable.
	}

	// Add fields to the cloned encoder
	for _, field := range fields {
		field.AddTo(clone.encoder)
	}

	return clone
}

func (c *LokiCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *LokiCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Encode log entry to JSON
	buf, err := c.encoder.EncodeEntry(ent, fields)
	if err != nil {
		return err
	}
	logLine := buf.String()

	// Timestamp in nanoseconds string
	ts := fmt.Sprintf("%d", ent.Time.UnixNano())

	// Send to channel (non-blocking if full to prevent app freeze, or blocking?
	// specific logging requirements usually prefer drop over block, but let's block slightly or drop)
	// For now, let's use a select with default to drop if super full, or just normal send.
	// The buffer is 1000, which is decent.

	select {
	case c.entryChan <- lokiEntry{
		Timestamp: ts,
		Line:      logLine,
		Level:     ent.Level.String(),
	}:
	default:
		// Channel full, drop log to prevent blocking the application
		// In production, might want internal metrics for this.
	}

	return nil
}

func (c *LokiCore) Sync() error {
	// We can't easily force flush the async worker without a control channel
	// But since this is often called on shutdown, we might want to wait.
	// For now, we leave it no-op or we could expose a Flush method.
	return nil
}

func (c *LokiCore) run() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.batchWait)
	defer ticker.Stop()

	var batch []lokiEntry

	flush := func() {
		if len(batch) == 0 {
			return
		}
		c.sendBatch(batch)
		batch = nil // clear batch (reallocate or slicing? slicing is better if avoiding gc, but let's just nil)
		batch = make([]lokiEntry, 0, c.batchSize)
	}

	for {
		select {
		case entry := <-c.entryChan:
			batch = append(batch, entry)
			if len(batch) >= c.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-c.shutdownCh:
			flush()
			return
		}
	}
}

func (c *LokiCore) sendBatch(entries []lokiEntry) {
	// Group by level (stream)
	streams := make(map[string][][]string)

	for _, e := range entries {
		streams[e.Level] = append(streams[e.Level], []string{e.Timestamp, e.Line})
	}

	var req lokiPushRequest
	for lvl, values := range streams {
		req.Streams = append(req.Streams, lokiStream{
			Stream: map[string]string{
				"app":   c.appName,
				"level": lvl,
			},
			Values: values,
		})
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return
	}

	resp, err := c.client.Post(c.url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Failed to push batch to Loki: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// Check response status if needed, but we ignore for now as per previous implementation
}
