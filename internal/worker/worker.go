package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gleison/webhook-proxy-go/internal/config"
	"github.com/gleison/webhook-proxy-go/internal/domain"
	"github.com/gleison/webhook-proxy-go/internal/infrastructure/queue"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

type Processor struct {
	NATS            *queue.NATSClient
	Log             *zap.Logger
	DestinationURLs []string
	Config          config.NATSConfig
	Client          *http.Client
}

func NewProcessor(nats *queue.NATSClient, log *zap.Logger, destURLs string, natsCfg config.NATSConfig) *Processor {
	// Optimized HTTP Client
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100

	client := &http.Client{
		Timeout:   30 * time.Second, // Global timeout
		Transport: t,
	}

	// Split and trim URLs
	var urls []string
	for _, u := range strings.Split(destURLs, ",") {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" {
			urls = append(urls, trimmed)
		}
	}

	return &Processor{
		NATS:            nats,
		Log:             log,
		DestinationURLs: urls,
		Config:          natsCfg,
		Client:          client,
	}
}

func (p *Processor) Start(ctx context.Context) error {
	p.Log.Info("Starting Worker Processor")

	// Create Consumer
	// Durable consumer specific to this processor
	cfg := jetstream.ConsumerConfig{
		Durable:       "WebhookProcessor",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: "webhooks.ingested",
		// Retry Policy
		MaxDeliver: p.Config.MaxRetries,
		AckWait:    time.Duration(p.Config.AckWaitSeconds) * time.Second,
		// Simple linear backoff for robustness (optional, requires NATS server 2.10+)
		// Backoff: []time.Duration{1 * time.Second, 5 * time.Second, 10 * time.Second},
	}

	cons, err := p.NATS.JS.CreateOrUpdateConsumer(ctx, "WEBHOOKS", cfg)
	if err != nil {
		p.Log.Error("Failed to create consumer", zap.Error(err))
		return err
	}

	// Consume
	iter, err := cons.Messages(jetstream.PullMaxMessages(10))
	if err != nil {
		return err
	}

	// Worker Pool
	concurrency := p.Config.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	// Channel for distributing messages to workers
	tasks := make(chan jetstream.Msg, concurrency) // buffer = concurrency

	// Spawn Workers
	for i := 0; i < concurrency; i++ {
		go func(workerID int) {
			p.Log.Info("Starting worker", zap.Int("worker_id", workerID))
			for msg := range tasks {
				p.processMessage(msg)
			}
		}(i)
	}

	// Dispatcher (Pulls from NATS and puts into tasks channel)
	go func() {
		defer close(tasks) // Close channel on exit
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Fetch next message
				msg, err := iter.Next()
				if err != nil {
					// Timeout or other error, just continue/backoff
					// Normally Next() blocks, so err means something happened.
					// We verify if it's just a timeout or connection issue.
					time.Sleep(10 * time.Millisecond) // Reduce sleep for tighter loop
					continue
				}

				// Send to worker pool
				select {
				case tasks <- msg:
					// submitted
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return nil
}

func (p *Processor) processMessage(msg jetstream.Msg) {
	meta, _ := msg.Metadata()
	p.Log.Info("Received message", zap.Uint64("sequence", meta.Sequence.Stream))

	var payload domain.WebhookPayload
	if err := json.Unmarshal(msg.Data(), &payload); err != nil {
		p.Log.Error("Failed to unmarshal message", zap.Error(err))
		msg.Ack()
		return
	}

	// Calculate Queue Duration (Latency)
	// Precision: UnixNano
	ingestedAt := time.Unix(0, payload.Timestamp)
	queueDuration := time.Since(ingestedAt)

	// Forward to destination
	if len(p.DestinationURLs) == 0 {
		p.Log.Warn("No destination URL configured, skipping forward", zap.String("webhook_id", payload.ID))
		msg.Ack()
		return
	}

	var lastErr error
	var lastStatus int
	var success bool

	for _, destURL := range p.DestinationURLs {
		// Construct dynamic destination
		baseURL := strings.TrimRight(destURL, "/")
		path := payload.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		finalURL := baseURL + path

		// Create request
		body, _ := json.Marshal(payload.Payload)

		// Measure Forward Duration
		startForward := time.Now()
		resp, err := p.Client.Post(finalURL, "application/json", bytes.NewBuffer(body))
		forwardDuration := time.Since(startForward)

		if err != nil {
			p.Log.Warn("Failed to forward webhook to url",
				zap.String("url", destURL),
				zap.Error(err),
			)
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		lastStatus = resp.StatusCode

		totalDuration := time.Since(ingestedAt)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			p.Log.Info("Webhook forwarded successfully",
				zap.String("webhook_id", payload.ID),
				zap.Int("status", resp.StatusCode),
				zap.Float64("queue_duration_ms", float64(queueDuration.Nanoseconds())/1e6),
				zap.Float64("forward_duration_ms", float64(forwardDuration.Nanoseconds())/1e6),
				zap.Float64("total_duration_ms", float64(totalDuration.Nanoseconds())/1e6),
				zap.String("destination", finalURL),
			)
			msg.Ack()
			success = true
			break // Stop on first success
		} else {
			p.Log.Error("Destination returned error",
				zap.String("webhook_id", payload.ID),
				zap.String("url", finalURL),
				zap.Int("status", resp.StatusCode),
				zap.Float64("queue_duration_ms", float64(queueDuration.Nanoseconds())/1e6),
				zap.Float64("forward_duration_ms", float64(forwardDuration.Nanoseconds())/1e6),
			)
			// Don't break immediately, try next URL if available
		}
	}

	if success {
		return
	}

	// If we are here, it means all URLs failed
	// We decide whether to retry based on the last error or status code
	// Logic: If at least one failure was retryable (network or 500), we retry.
	// For simplicity, if we exhausted all options and didn't succeed, we retry unless it's a permanent 4xx on the last attempt.

	shouldRetry := false
	if lastErr != nil {
		shouldRetry = true
	} else if lastStatus >= 500 {
		shouldRetry = true
	}

	if shouldRetry {
		// Calculate Exponential Backoff
		retries := float64(meta.NumDelivered - 1)
		if retries < 0 {
			retries = 0
		}

		multiplier := p.Config.RetryBackoffMultiplier
		if multiplier < 1.0 {
			multiplier = 1.0
		}

		base := float64(p.Config.RetryBackoffSeconds)
		delaySeconds := base * math.Pow(multiplier, retries)

		backoff := time.Duration(delaySeconds) * time.Second

		p.Log.Warn("Failed to forward webhook to all destinations, retrying...",
			zap.Error(lastErr),
			zap.Int("last_status", lastStatus),
			zap.String("webhook_id", payload.ID),
			zap.Uint64("attempt", meta.NumDelivered),
			zap.Float64("next_retry_in_seconds", backoff.Seconds()),
		)
		msg.NakWithDelay(backoff)
	} else {
		// Permanent failure (e.g. 4xx from last URL)
		p.Log.Error("Failed to forward webhook to all destinations, giving up",
			zap.Int("last_status", lastStatus),
			zap.String("webhook_id", payload.ID),
		)
		msg.Term()
	}
}
