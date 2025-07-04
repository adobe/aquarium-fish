/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

// Package monitoring provides OpenTelemetry-based observability for Aquarium Fish
package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/log/global"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	otellog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

const (
	serviceName    = "aquarium-fish"
	serviceVersion = "1.0.0" // This will be updated with build info
)

// Config defines monitoring configuration
type Config struct {
	Enabled         bool          `json:"enabled"`          // Enable/disable monitoring
	OTLPEndpoint    string        `json:"otlp_endpoint"`    // OTLP endpoint for traces, metrics, logs
	PyroscopeURL    string        `json:"pyroscope_url"`    // Pyroscope URL for profiling
	ServiceName     string        `json:"service_name"`     // Service name for telemetry
	ServiceVersion  string        `json:"service_version"`  // Service version
	NodeUID         string        `json:"node_uid"`         // Node UID for resource attributes
	NodeName        string        `json:"node_name"`        // Node name for resource attributes
	NodeLocation    string        `json:"node_location"`    // Node location for resource attributes
	SampleRate      float64       `json:"sample_rate"`      // Trace sampling rate (0.0 to 1.0)
	MetricsInterval util.Duration `json:"metrics_interval"` // Metrics collection interval
	EnableProfiling bool          `json:"enable_profiling"` // Enable profiling
	EnableTracing   bool          `json:"enable_tracing"`   // Enable tracing
	EnableMetrics   bool          `json:"enable_metrics"`   // Enable metrics
	EnableLogs      bool          `json:"enable_logs"`      // Enable logs
}

// DefaultConfig returns default monitoring configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:         false, // Disabled by default
		OTLPEndpoint:    "localhost:4317",
		PyroscopeURL:    "http://localhost:4040",
		ServiceName:     serviceName,
		ServiceVersion:  serviceVersion,
		SampleRate:      1.0, // 100% sampling for development
		MetricsInterval: util.Duration(15 * time.Second),
		EnableProfiling: true,
		EnableTracing:   true,
		EnableMetrics:   true,
		EnableLogs:      true,
	}
}

// Monitor represents the monitoring system
type Monitor struct {
	config         *Config
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider
	loggerProvider *otellog.LoggerProvider
	promExporter   *prometheus.Exporter
	pyroscope      *pyroscope.Profiler
	tracer         oteltrace.Tracer
	meter          otelmetric.Meter
	metrics        *Metrics
	shutdownFuncs  []func(context.Context) error
}

// Initialize sets up OpenTelemetry monitoring
func Initialize(ctx context.Context, config *Config) (*Monitor, error) {
	if !config.Enabled {
		log.Info("Monitoring: Disabled")
		return &Monitor{config: config}, nil
	}

	log.Info("Monitoring: Initializing OpenTelemetry...")

	m := &Monitor{
		config:        config,
		shutdownFuncs: make([]func(context.Context) error, 0),
	}

	// Create resource with service information
	res, err := m.createResource()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Initialize tracing
	if config.EnableTracing {
		if err := m.initTracing(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to initialize tracing: %w", err)
		}
		log.Info("Monitoring: Tracing initialized")
	}

	// Initialize metrics
	if config.EnableMetrics {
		if err := m.initMetrics(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
		log.Info("Monitoring: Metrics initialized")
	}

	// Initialize logging
	if config.EnableLogs {
		if err := m.initLogging(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to initialize logging: %w", err)
		}
		log.Info("Monitoring: Logging initialized")
	}

	// Initialize profiling
	if config.EnableProfiling {
		if err := m.initProfiling(); err != nil {
			return nil, fmt.Errorf("failed to initialize profiling: %w", err)
		}
		log.Info("Monitoring: Profiling initialized")
	}

	// Initialize metrics collection
	if config.EnableMetrics {
		if err := m.initMetricsCollection(); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics collection: %w", err)
		}
		log.Info("Monitoring: Metrics collection initialized")
	}

	log.Info("Monitoring: OpenTelemetry initialization complete")
	return m, nil
}

// createResource creates an OpenTelemetry resource with service information
func (m *Monitor) createResource() (*resource.Resource, error) {
	res1 := resource.Default()
	res2 := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(m.config.ServiceName),
		semconv.ServiceVersion(m.config.ServiceVersion),
		attribute.String("node.name", m.config.NodeName),
		attribute.String("node.uid", m.config.NodeUID),
		attribute.String("node.location", m.config.NodeLocation),
	)
	log.Debugf("Monitoring: Merging 2 resources: %s and %s", res1, res2)
	return resource.Merge(res1, res2)
}

// initTracing initializes OpenTelemetry tracing
func (m *Monitor) initTracing(ctx context.Context, res *resource.Resource) error {
	// Create OTLP trace exporter
	conn, err := grpc.NewClient(m.config.OTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create trace provider
	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
		trace.WithSampler(trace.TraceIDRatioBased(m.config.SampleRate)),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	m.tracerProvider = tracerProvider
	m.tracer = otel.Tracer(m.config.ServiceName)

	m.shutdownFuncs = append(m.shutdownFuncs, tracerProvider.Shutdown)

	return nil
}

// initMetrics initializes OpenTelemetry metrics
func (m *Monitor) initMetrics(ctx context.Context, res *resource.Resource) error {
	// Create OTLP metrics exporter
	conn, err := grpc.NewClient(m.config.OTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	if err != nil {
		return fmt.Errorf("failed to create metrics exporter: %w", err)
	}

	// Create Prometheus exporter for local scraping
	promExporter, err := prometheus.New()
	if err != nil {
		return fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	m.promExporter = promExporter

	// Create meter provider with both exporters
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(time.Duration(m.config.MetricsInterval)))),
		metric.WithReader(promExporter),
	)

	otel.SetMeterProvider(meterProvider)
	m.meterProvider = meterProvider
	m.meter = otel.Meter(m.config.ServiceName)

	m.shutdownFuncs = append(m.shutdownFuncs, meterProvider.Shutdown)

	return nil
}

// initLogging initializes OpenTelemetry logging
func (m *Monitor) initLogging(ctx context.Context, res *resource.Resource) error {
	// Create OTLP log exporter
	conn, err := grpc.NewClient(m.config.OTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	logExporter, err := otlploggrpc.New(ctx, otlploggrpc.WithGRPCConn(conn))
	if err != nil {
		return fmt.Errorf("failed to create log exporter: %w", err)
	}

	// Create logger provider
	loggerProvider := otellog.NewLoggerProvider(
		otellog.WithProcessor(otellog.NewBatchProcessor(logExporter)),
		otellog.WithResource(res),
	)

	global.SetLoggerProvider(loggerProvider)
	m.loggerProvider = loggerProvider

	m.shutdownFuncs = append(m.shutdownFuncs, loggerProvider.Shutdown)

	return nil
}

// initProfiling initializes Pyroscope profiling
func (m *Monitor) initProfiling() error {
	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: m.config.ServiceName,
		ServerAddress:   m.config.PyroscopeURL,
		Tags: map[string]string{
			"node_name":     m.config.NodeName,
			"node_uid":      m.config.NodeUID,
			"node_location": m.config.NodeLocation,
			"version":       m.config.ServiceVersion,
		},
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to start pyroscope: %w", err)
	}

	m.pyroscope = profiler
	m.shutdownFuncs = append(m.shutdownFuncs, func(ctx context.Context) error {
		return profiler.Stop()
	})

	return nil
}

// initMetricsCollection initializes custom metrics collection
func (m *Monitor) initMetricsCollection() error {
	var err error
	m.metrics, err = NewMetrics(m.meter)
	if err != nil {
		return fmt.Errorf("failed to create metrics: %w", err)
	}

	return nil
}

// GetTracer returns the OpenTelemetry tracer
func (m *Monitor) GetTracer() oteltrace.Tracer {
	return m.tracer
}

// GetMeter returns the OpenTelemetry meter
func (m *Monitor) GetMeter() otelmetric.Meter {
	return m.meter
}

// GetMetrics returns the metrics collection
func (m *Monitor) GetMetrics() *Metrics {
	return m.metrics
}

// GetPrometheusHandler returns the Prometheus HTTP handler
func (m *Monitor) GetPrometheusHandler() *prometheus.Exporter {
	return m.promExporter
}

// StartSpan starts a new span with the given name
func (m *Monitor) StartSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	if m.tracer == nil {
		return ctx, oteltrace.SpanFromContext(ctx)
	}
	return m.tracer.Start(ctx, name, opts...)
}

// RecordMetric records a metric value
func (m *Monitor) RecordMetric(ctx context.Context, name string, value float64, attrs ...attribute.KeyValue) {
	if m.metrics == nil {
		return
	}
	// This will be implemented by specific metric recorders
}

// Shutdown gracefully shuts down the monitoring system
func (m *Monitor) Shutdown(ctx context.Context) error {
	log.Info("Monitoring: Shutting down...")

	var errs []error
	for _, shutdown := range m.shutdownFuncs {
		if err := shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	log.Info("Monitoring: Shutdown complete")
	return nil
}

// IsEnabled returns whether monitoring is enabled
func (m *Monitor) IsEnabled() bool {
	return m.config.Enabled
}
