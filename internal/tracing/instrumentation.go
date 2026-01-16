package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "cbox-init"
)

// StartProcessManagerSpan creates a span for process manager operations
func StartProcessManagerSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(instrumentationName)
	return tracer.Start(ctx, "process_manager."+operation, trace.WithAttributes(attrs...))
}

// StartProcessSpan creates a span for individual process operations
func StartProcessSpan(ctx context.Context, processName, operation string, instanceID int, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(instrumentationName)
	attrs = append(attrs,
		attribute.String("process.name", processName),
		attribute.String("process.operation", operation),
		attribute.Int("process.instance_id", instanceID),
	)
	return tracer.Start(ctx, "process."+operation, trace.WithAttributes(attrs...))
}

// StartSupervisorSpan creates a span for supervisor operations
func StartSupervisorSpan(ctx context.Context, processName, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(instrumentationName)
	attrs = append(attrs,
		attribute.String("supervisor.process_name", processName),
		attribute.String("supervisor.operation", operation),
	)
	return tracer.Start(ctx, "supervisor."+operation, trace.WithAttributes(attrs...))
}

// StartHealthCheckSpan creates a span for health check operations
func StartHealthCheckSpan(ctx context.Context, processName, checkType string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(instrumentationName)
	attrs = append(attrs,
		attribute.String("health_check.process_name", processName),
		attribute.String("health_check.type", checkType),
	)
	return tracer.Start(ctx, "health_check.execute", trace.WithAttributes(attrs...))
}

// RecordError records an error on the span
func RecordError(span trace.Span, err error, description string) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err, trace.WithAttributes(
		attribute.String("error.description", description),
	))
	span.SetStatus(codes.Error, description)
}

// RecordSuccess marks the span as successful
func RecordSuccess(span trace.Span) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Ok, "")
}

// AddEvent adds an event to the span
func AddEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	if span == nil {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets additional attributes on the span
func SetAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span == nil {
		return
	}
	span.SetAttributes(attrs...)
}
