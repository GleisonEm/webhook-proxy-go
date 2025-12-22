package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

// InitLogger initializes the global logger with a potential sink to Loki (via stdout/promtail or direct push if implemented)
// For now, we will output JSON to stdout, which Promtail can pick up and push to Loki.
// Direct HTTP push to Loki from here is possible but often better decoupled via Promtail.
// However, the user asked to "Connect logs" and has Loki.
// Simulating direct connection might be complex without a library, so standard JSON out is best practice for containerized apps.
func InitLogger(level string, encoding string, appName string, lokiURL string) error {
	atomicLevel := zap.NewAtomicLevel()
	l, err := zapcore.ParseLevel(level)
	if err != nil {
		return err
	}
	atomicLevel.SetLevel(l)

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if encoding == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.Lock(os.Stdout),
		atomicLevel,
	)

	cores := []zapcore.Core{core}

	// Add Loki Core if URL is configured
	if lokiURL != "" {
		// Use the same level as main config, or info default
		lokiCore := NewLokiCore(l, lokiURL, appName)
		cores = append(cores, lokiCore)
	}

	// Combine cores
	combinedCore := zapcore.NewTee(cores...)

	// Include app_name in every log
	Log = zap.New(combinedCore, zap.AddCaller(), zap.Fields(zap.String("app_name", appName)))
	zap.ReplaceGlobals(Log)

	return nil
}
