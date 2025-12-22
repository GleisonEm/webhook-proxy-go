package queue

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

type NATSClient struct {
	Conn *nats.Conn
	JS   jetstream.JetStream
	Log  *zap.Logger
}

func NewNATSClient(url string, log *zap.Logger) (*NATSClient, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	return &NATSClient{
		Conn: nc,
		JS:   js,
		Log:  log,
	}, nil
}

func (n *NATSClient) Close() {
	if n.Conn != nil {
		n.Conn.Close()
	}
}

// Publish sends a message to a subject synchronously
func (n *NATSClient) Publish(subject string, data []byte) error {
	_, err := n.JS.Publish(context.Background(), subject, data)
	if err != nil {
		n.Log.Error("Failed to publish message", zap.String("subject", subject), zap.Error(err))
		return err
	}
	return nil
}

// PublishAsync sends a message to a subject asynchronously for higher throughput
func (n *NATSClient) PublishAsync(subject string, data []byte) error {
	_, err := n.JS.PublishAsync(subject, data)
	if err != nil {
		n.Log.Error("Failed to async publish message", zap.String("subject", subject), zap.Error(err))
		return err
	}
	return nil
}

// EnsureStream creates a stream if it doesn't exist
func (n *NATSClient) EnsureStream(name string, subjects []string) error {
	ctx := context.Background()
	_, err := n.JS.Stream(ctx, name)
	if err == nil {
		return nil // Stream exists
	}

	// Stream Config
	// Reverting to FileStorage for Robustness as requested initially.
	// We handle the "Speed" via PublishAsync in the handler.
	cfg := jetstream.StreamConfig{
		Name:     name,
		Subjects: subjects,
		Storage:  jetstream.FileStorage,
	}

	_, err = n.JS.CreateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	n.Log.Info("Created JetStream stream", zap.String("stream", name))
	return nil
}
