package tracing

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestTracerConfig_Default(t *testing.T) {
	cfg := TracerConfig{}

	if cfg.Enabled {
		t.Error("Default Enabled should be false")
	}
	if cfg.SampleRate != 0 {
		t.Errorf("Default SampleRate should be 0, got %f", cfg.SampleRate)
	}
}

func TestNewProvider_Disabled(t *testing.T) {
	cfg := TracerConfig{
		Enabled: false,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	if provider.Enabled() {
		t.Error("Provider should not be enabled when config.Enabled is false")
	}

	// Shutdown should be a no-op when disabled
	err = provider.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestNewProvider_UnsupportedExporter(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "unsupported",
		ServiceName: "test-service",
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	_, err := NewProvider(context.Background(), cfg, logger)
	if err == nil {
		t.Error("Expected error for unsupported exporter")
	}
}

func TestNewProvider_Stdout(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()

	if !provider.Enabled() {
		t.Error("Provider should be enabled with stdout exporter")
	}
}

func TestProvider_Tracer_Disabled(t *testing.T) {
	provider := &Provider{
		tp:     nil,
		logger: slog.Default(),
	}

	tracer := provider.Tracer("test")
	if tracer == nil {
		t.Error("Tracer should not be nil even when disabled")
	}

	// Should return a noop tracer
	ctx, span := tracer.Start(context.Background(), "test-span")
	if ctx == nil || span == nil {
		t.Error("Noop tracer should return valid context and span")
	}
	span.End()
}

func TestProvider_Tracer_Enabled(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()

	tracer := provider.Tracer("test-component")
	if tracer == nil {
		t.Error("Tracer should not be nil")
	}

	// Create a span
	ctx, span := tracer.Start(context.Background(), "test-operation")
	if ctx == nil || span == nil {
		t.Error("Start should return valid context and span")
	}
	span.End()
}

func TestProvider_Shutdown(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	err = provider.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestSamplerRates(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate float64
	}{
		{"always_sample", 1.0},
		{"never_sample", 0.0},
		{"ratio_sample", 0.5},
		{"above_one", 1.5},   // Should behave as always sample
		{"below_zero", -0.5}, // Should behave as never sample
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := TracerConfig{
				Enabled:     true,
				Exporter:    "stdout",
				ServiceName: "test-service",
				SampleRate:  tt.sampleRate,
			}

			provider, err := NewProvider(context.Background(), cfg, logger)
			if err != nil {
				t.Fatalf("NewProvider failed: %v", err)
			}
			defer func() { _ = provider.Shutdown(context.Background()) }()

			if !provider.Enabled() {
				t.Error("Provider should be enabled")
			}
		})
	}
}

// Instrumentation tests

func TestStartProcessManagerSpan(t *testing.T) {
	ctx, span := StartProcessManagerSpan(context.Background(), "start",
		attribute.Int("process.count", 5))

	if ctx == nil {
		t.Error("Context should not be nil")
	}
	if span == nil {
		t.Error("Span should not be nil")
	}
	span.End()
}

func TestStartProcessSpan(t *testing.T) {
	ctx, span := StartProcessSpan(context.Background(), "php-fpm", "start", 0,
		attribute.String("process.status", "running"))

	if ctx == nil {
		t.Error("Context should not be nil")
	}
	if span == nil {
		t.Error("Span should not be nil")
	}
	span.End()
}

func TestStartSupervisorSpan(t *testing.T) {
	ctx, span := StartSupervisorSpan(context.Background(), "nginx", "restart",
		attribute.Int("restart.attempt", 1))

	if ctx == nil {
		t.Error("Context should not be nil")
	}
	if span == nil {
		t.Error("Span should not be nil")
	}
	span.End()
}

func TestStartHealthCheckSpan(t *testing.T) {
	ctx, span := StartHealthCheckSpan(context.Background(), "api", "http",
		attribute.String("health_check.url", "http://localhost:8080/health"))

	if ctx == nil {
		t.Error("Context should not be nil")
	}
	if span == nil {
		t.Error("Span should not be nil")
	}
	span.End()
}

func TestRecordError_NilSpan(t *testing.T) {
	// Should not panic with nil span
	RecordError(nil, errors.New("test error"), "test description")
}

func TestRecordError_NilError(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	// Should not record anything with nil error
	RecordError(span, nil, "test description")
}

func TestRecordError(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	err := errors.New("test error")
	RecordError(span, err, "test description")
	// If we get here without panic, the test passes
}

func TestRecordSuccess_NilSpan(t *testing.T) {
	// Should not panic with nil span
	RecordSuccess(nil)
}

func TestRecordSuccess(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	RecordSuccess(span)
	// If we get here without panic, the test passes
}

func TestAddEvent_NilSpan(t *testing.T) {
	// Should not panic with nil span
	AddEvent(nil, "test event", attribute.String("key", "value"))
}

func TestAddEvent(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	AddEvent(span, "process_started",
		attribute.String("process.name", "nginx"),
		attribute.Int("process.pid", 12345))
	// If we get here without panic, the test passes
}

func TestSetAttributes_NilSpan(t *testing.T) {
	// Should not panic with nil span
	SetAttributes(nil, attribute.String("key", "value"))
}

func TestSetAttributes(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	SetAttributes(span,
		attribute.String("custom.key1", "value1"),
		attribute.Int("custom.key2", 42))
	// If we get here without panic, the test passes
}

func TestTracerConfig_ServiceVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"with_version", "1.0.0"},
		{"empty_version", ""}, // Should default to "unknown"
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := TracerConfig{
				Enabled:     true,
				Exporter:    "stdout",
				ServiceName: "test-service",
				Version:     tt.version,
				SampleRate:  1.0,
			}

			provider, err := NewProvider(context.Background(), cfg, logger)
			if err != nil {
				t.Fatalf("NewProvider failed: %v", err)
			}
			defer func() { _ = provider.Shutdown(context.Background()) }()
		})
	}
}

func TestNewProvider_OTLPGrpc_Insecure(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "otlp-grpc",
		Endpoint:    "localhost:4317",
		ServiceName: "test-service",
		SampleRate:  1.0,
		UseTLS:      false, // insecure mode
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// This will create the exporter successfully even without a running server
	// The connection won't be established until traces are sent
	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()

	if !provider.Enabled() {
		t.Error("Provider should be enabled with otlp-grpc exporter")
	}
}

func TestNewProvider_OTLPGrpc_WithTLS(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "otlp-grpc",
		Endpoint:    "localhost:4317",
		ServiceName: "test-service",
		SampleRate:  1.0,
		UseTLS:      true, // TLS mode
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// This will create the exporter successfully even without a running server
	// The connection won't be established until traces are sent
	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()

	if !provider.Enabled() {
		t.Error("Provider should be enabled with otlp-grpc exporter")
	}
}

func TestCreateOTLPGRPCExporter_Insecure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	exporter, err := createOTLPGRPCExporter(context.Background(), "localhost:4317", false, logger)
	if err != nil {
		t.Fatalf("createOTLPGRPCExporter failed: %v", err)
	}
	if exporter == nil {
		t.Error("Expected non-nil exporter")
	}
	// Cleanup
	if exporter != nil {
		_ = exporter.Shutdown(context.Background())
	}
}

func TestCreateOTLPGRPCExporter_WithTLS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	exporter, err := createOTLPGRPCExporter(context.Background(), "localhost:4317", true, logger)
	if err != nil {
		t.Fatalf("createOTLPGRPCExporter failed: %v", err)
	}
	if exporter == nil {
		t.Error("Expected non-nil exporter")
	}
	// Cleanup
	if exporter != nil {
		_ = exporter.Shutdown(context.Background())
	}
}

func TestCreateExporter_OTLPGrpc(t *testing.T) {
	cfg := TracerConfig{
		Exporter: "otlp-grpc",
		Endpoint: "localhost:4317",
		UseTLS:   false,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	exporter, err := createExporter(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("createExporter failed: %v", err)
	}
	if exporter == nil {
		t.Error("Expected non-nil exporter")
	}
	// Cleanup
	if exporter != nil {
		_ = exporter.Shutdown(context.Background())
	}
}

func TestCreateExporter_Stdout(t *testing.T) {
	cfg := TracerConfig{
		Exporter: "stdout",
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	exporter, err := createExporter(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("createExporter failed: %v", err)
	}
	if exporter == nil {
		t.Error("Expected non-nil exporter")
	}
}

func TestCreateExporter_Unsupported(t *testing.T) {
	cfg := TracerConfig{
		Exporter: "invalid",
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	_, err := createExporter(context.Background(), cfg, logger)
	if err == nil {
		t.Error("Expected error for unsupported exporter")
	}
}

func TestProvider_Shutdown_WithContext(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	// Create some spans before shutdown
	tracer := provider.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()

	// Shutdown with context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestProvider_Enabled_WhenDisabled(t *testing.T) {
	provider := &Provider{
		tp:     nil,
		logger: slog.Default(),
	}

	if provider.Enabled() {
		t.Error("Provider should not be enabled when tp is nil")
	}
}

func TestCreateStdoutExporter(t *testing.T) {
	exporter, err := createStdoutExporter()
	if err != nil {
		t.Fatalf("createStdoutExporter failed: %v", err)
	}
	if exporter == nil {
		t.Error("Expected non-nil exporter")
	}
}

func TestProvider_Shutdown_WithCancelledContext(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	// Create some spans to flush
	tracer := provider.Tracer("test")
	for i := 0; i < 100; i++ {
		_, span := tracer.Start(context.Background(), "test-span")
		span.End()
	}

	// Try shutdown with already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This may or may not error depending on timing, but exercises the shutdown path
	_ = provider.Shutdown(ctx)
}

func TestProvider_Shutdown_WithExpiredContext(t *testing.T) {
	cfg := TracerConfig{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	provider, err := NewProvider(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	// Create spans
	tracer := provider.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()

	// Use very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // Ensure context is expired

	// Shutdown with expired context - may trigger error path
	_ = provider.Shutdown(ctx)
}
