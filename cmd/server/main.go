package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gleison/webhook-proxy-go/internal/config"
	"github.com/gleison/webhook-proxy-go/internal/infrastructure/logger" // actually zap.go package is logger
	"github.com/gleison/webhook-proxy-go/internal/infrastructure/queue"
	"github.com/gleison/webhook-proxy-go/internal/interface/http"
	"github.com/gleison/webhook-proxy-go/internal/worker"
	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// 0. Load .env file (Robust check: CWD and Root)
	_ = godotenv.Load()             // from CWD
	_ = godotenv.Load("../../.env") // from Root (if running from cmd/server)

	// 1. Load Config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Init Logger
	if err := logger.InitLogger(cfg.Log.Level, cfg.Log.Encoding, cfg.Log.AppName, cfg.Log.LokiURL); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	logg := logger.Log
	defer logg.Sync()

	logg.Info("Starting Webhook Proxy", zap.Any("config", cfg))

	// 3. Init NATS
	natsClient, err := queue.NewNATSClient(cfg.NATS.URL, logg)
	if err != nil {
		logg.Fatal("Failed to connect to NATS", zap.Error(err))
	}
	defer natsClient.Close()

	// Ensure Stream Exists
	err = natsClient.EnsureStream("WEBHOOKS", []string{"webhooks.>"})
	if err != nil {
		logg.Fatal("Failed to ensure stream", zap.Error(err))
	}

	// 4. Init Fiber
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	// 5. Register Routes
	handler := http.NewHandler(natsClient, logg)
	handler.RegisterRoutes(app)

	// 6. Start Worker
	proc := worker.NewProcessor(natsClient, logg, cfg.Server.DestinationURL, cfg.NATS)
	if err := proc.Start(context.Background()); err != nil {
		logg.Error("Failed to start worker", zap.Error(err))
	}

	// 7. Start Server
	go func() {
		if err := app.Listen(":" + cfg.Server.Port); err != nil {
			logg.Fatal("Server failed", zap.Error(err))
		}
	}()

	logg.Info("Server started", zap.String("port", cfg.Server.Port))

	// Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	logg.Info("Shutting down...")
	_ = app.Shutdown()
}
