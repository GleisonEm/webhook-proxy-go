package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

func NewLokiCore(level zapcore.Level, url, appName string) *LokiCore {
	return &LokiCore{
		LevelEnabler: level,
		encoder:      zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		url:          url + "/loki/api/v1/push",
		appName:      appName,
		client:       &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *LokiCore) With(fields []zapcore.Field) zapcore.Core {
	return &LokiCore{
		LevelEnabler: c.LevelEnabler,
		encoder:      c.encoder.Clone(),
		url:          c.url,
		appName:      c.appName,
		client:       c.client,
	}
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

	// Prepare Loki Payload
	// Timestamp in nanoseconds string
	ts := fmt.Sprintf("%d", ent.Time.UnixNano())

	payload := lokiPushRequest{
		Streams: []lokiStream{
			{
				Stream: map[string]string{
					"app":   c.appName,
					"level": ent.Level.String(),
				},
				Values: [][]string{
					{ts, logLine},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Fire and forget (in a goroutine to not block main path)
	go func() {
		resp, err := c.client.Post(c.url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			// Silently fail or print to stderr
			fmt.Printf("Failed to push to Loki: %v\n", err)
			return
		}
		defer resp.Body.Close()
	}()

	return nil
}

func (c *LokiCore) Sync() error {
	return nil
}
