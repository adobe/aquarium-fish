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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/grafana/pyroscope-go"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
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
	Enabled         bool `json:"enabled"`          // Enable/disable monitoring
	EnableLogs      bool `json:"enable_logs"`      // Enable logs
	EnableMetrics   bool `json:"enable_metrics"`   // Enable metrics
	EnableProfiling bool `json:"enable_profiling"` // Enable profiling
	EnableTracing   bool `json:"enable_tracing"`   // Enable tracing

	OTLPEndpoint   string `json:"otlp_endpoint"`    // OTLP endpoint for traces, metrics, logs
	PyroscopeURL   string `json:"pyroscope_url"`    // Pyroscope URL for profiling
	FileExportPath string `json:"file_export_path"` // Path to export telemetry data to files (when no remote endpoints configured)

	ServiceName    string `json:"service_name"`    // Service name for telemetry
	ServiceVersion string `json:"service_version"` // Service version

	SampleRate        float64       `json:"sample_rate"`        // Trace sampling rate (0.0 to 1.0)
	MetricsInterval   util.Duration `json:"metrics_interval"`   // Metrics collection interval
	ProfilingInterval util.Duration `json:"profiling_interval"` // Profiling file export interval

	// Will be set automatically from fish
	NodeUID      string `json:"node_uid"`      // Node UID for resource attributes
	NodeName     string `json:"node_name"`     // Node name for resource attributes
	NodeLocation string `json:"node_location"` // Node location for resource attributes
}

// DefaultConfig returns default monitoring configuration
func (c *Config) InitDefaults() {
	c.Enabled = false // Disabled by default
	c.EnableLogs = true
	c.EnableMetrics = true
	c.EnableProfiling = true
	c.EnableTracing = true

	c.OTLPEndpoint = ""
	c.PyroscopeURL = ""
	c.FileExportPath = "" // Will be set to fish ws directory + "telemetry" if not configured

	c.ServiceName = serviceName
	c.ServiceVersion = serviceVersion

	c.SampleRate = 1.0 // 100% sampling for development
	c.MetricsInterval = util.Duration(15 * time.Second)
	c.ProfilingInterval = util.Duration(30 * time.Second)
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

	shutdownFuncs []func(context.Context) error
	shutdownCh    chan struct{}
}

// Initialize sets up OpenTelemetry monitoring
func Initialize(ctx context.Context, config *Config) (*Monitor, error) {
	if !config.Enabled {
		log.Info().Msg("Monitoring: Disabled")
		return &Monitor{config: config}, nil
	}

	log.Info().Msg("Monitoring: Initializing OpenTelemetry...")

	m := &Monitor{
		config:        config,
		shutdownFuncs: make([]func(context.Context) error, 0),
		shutdownCh:    make(chan struct{}),
	}

	// Create resource with service information
	res, err := m.createResource()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Setup file export path if needed
	if err := m.setupFileExport(); err != nil {
		return nil, fmt.Errorf("failed to setup file export: %w", err)
	}

	// Initialize tracing
	if config.EnableTracing {
		if err := m.initTracing(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to initialize tracing: %w", err)
		}
		log.Info().Msg("Monitoring: Tracing initialized")
	}

	// Initialize metrics
	if config.EnableMetrics {
		if err := m.initMetrics(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
		log.Info().Msg("Monitoring: Metrics initialized")
	}

	// Initialize logging
	if config.EnableLogs {
		if err := m.initLogging(ctx, res); err != nil {
			return nil, fmt.Errorf("failed to initialize logging: %w", err)
		}
		// Setup OpenTelemetry integration with zerolog
		log.SetupOtelIntegration("info")
		log.Info().Msg("Monitoring: Logging initialized")
	}

	// Initialize profiling
	if config.EnableProfiling {
		if err := m.initProfiling(); err != nil {
			return nil, fmt.Errorf("failed to initialize profiling: %w", err)
		}
		log.Info().Msg("Monitoring: Profiling initialized")
	}

	// Initialize metrics collection
	if config.EnableMetrics {
		if err := m.initMetricsCollection(); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics collection: %w", err)
		}
		log.Info().Msg("Monitoring: Metrics collection initialized")
	}

	log.Info().Msg("Monitoring: OpenTelemetry initialization complete")
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
	log.Debug().Msgf("Monitoring: Merging 2 resources: %s and %s", res1, res2)
	return resource.Merge(res1, res2)
}

// setupFileExport sets up file export paths and creates directories if needed
func (m *Monitor) setupFileExport() error {
	if m.config.FileExportPath == "" {
		return nil // No file export configured
	}

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(m.config.FileExportPath, 0755); err != nil {
		return fmt.Errorf("failed to create file export directory: %w", err)
	}

	// Create subdirectories for different telemetry types
	subdirs := []string{"traces", "metrics", "logs", "profiling"}
	for _, subdir := range subdirs {
		fullPath := filepath.Join(m.config.FileExportPath, subdir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", subdir, err)
		}
	}

	log.Info().Msgf("Monitoring: File export path set to %s", m.config.FileExportPath)
	return nil
}

// shouldUseFileExport returns true if file export should be used instead of remote endpoints
func (m *Monitor) shouldUseFileExport() bool {
	return m.config.FileExportPath != "" && m.config.OTLPEndpoint == ""
}

// initTracing initializes OpenTelemetry tracing
func (m *Monitor) initTracing(ctx context.Context, res *resource.Resource) error {
	var traceExporter trace.SpanExporter
	var err error

	if m.shouldUseFileExport() {
		// Create file-based trace exporter
		tracesFile := filepath.Join(m.config.FileExportPath, "traces", "traces.jsonl")
		traceExporter, err = NewFileTraceExporter(tracesFile)
		if err != nil {
			return fmt.Errorf("failed to create file trace exporter: %w", err)
		}
		log.Info().Msgf("Monitoring: Using file trace exporter: %s", tracesFile)
	} else {
		// Create OTLP trace exporter
		conn, err := grpc.NewClient(m.config.OTLPEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("failed to create gRPC connection: %w", err)
		}

		traceExporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
		if err != nil {
			return fmt.Errorf("failed to create trace exporter: %w", err)
		}
		log.Info().Msgf("Monitoring: Using OTLP trace exporter: %s", m.config.OTLPEndpoint)
	}

	// Create trace provider
	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
		trace.WithSampler(trace.TraceIDRatioBased(m.config.SampleRate)),
	)

	// Wrap with profiling tracer provider for OpenTelemetry profiling integration
	profilingTracerProvider := otelpyroscope.NewTracerProvider(tracerProvider)

	otel.SetTracerProvider(profilingTracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	m.tracerProvider = tracerProvider
	m.tracer = otel.Tracer(m.config.ServiceName)

	m.shutdownFuncs = append(m.shutdownFuncs, tracerProvider.Shutdown)

	return nil
}

// initMetrics initializes OpenTelemetry metrics
func (m *Monitor) initMetrics(ctx context.Context, res *resource.Resource) error {
	if m.shouldUseFileExport() {
		// Create Prometheus exporter for local scraping
		promExporter, err := prometheus.New()
		if err != nil {
			return fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		m.promExporter = promExporter

		// Create meter provider with Prometheus exporter for file export mode
		meterProvider := metric.NewMeterProvider(
			metric.WithResource(res),
			metric.WithReader(promExporter),
		)

		// Set up periodic metrics file export using Prometheus data
		metricsFile := filepath.Join(m.config.FileExportPath, "metrics", "metrics.jsonl")
		go m.startPeriodicMetricsFileExport(metricsFile)

		otel.SetMeterProvider(meterProvider)
		m.meterProvider = meterProvider
		m.meter = otel.Meter(m.config.ServiceName)

		m.shutdownFuncs = append(m.shutdownFuncs, meterProvider.Shutdown)

		log.Info().Msgf("Monitoring: Using file metrics exporter: %s", metricsFile)
	} else {
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

		log.Info().Msgf("Monitoring: Using OTLP metrics exporter: %s", m.config.OTLPEndpoint)
	}

	return nil
}

// initLogging initializes OpenTelemetry logging
func (m *Monitor) initLogging(ctx context.Context, res *resource.Resource) error {
	if m.shouldUseFileExport() {
		// Create file-based log exporter
		logsFile := filepath.Join(m.config.FileExportPath, "logs", "logs.jsonl")
		logExporter, err := NewFileLogExporter(logsFile)
		if err != nil {
			return fmt.Errorf("failed to create file log exporter: %w", err)
		}

		// Create logger provider
		loggerProvider := otellog.NewLoggerProvider(
			otellog.WithProcessor(otellog.NewBatchProcessor(logExporter)),
			otellog.WithResource(res),
		)

		global.SetLoggerProvider(loggerProvider)
		m.loggerProvider = loggerProvider

		m.shutdownFuncs = append(m.shutdownFuncs, loggerProvider.Shutdown)

		log.Info().Msgf("Monitoring: Using file log exporter: %s", logsFile)
	} else {
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

		log.Info().Msgf("Monitoring: Using OTLP log exporter: %s", m.config.OTLPEndpoint)
	}

	return nil
}

// initProfiling initializes profiling with OpenTelemetry integration
func (m *Monitor) initProfiling() error {
	if m.shouldUseFileExport() {
		// Use file-based profiling when no remote endpoint is configured
		return m.initFileBasedProfiling()
	} else {
		// Use Pyroscope with OpenTelemetry integration for remote profiling
		return m.initRemoteProfiling()
	}
}

// initRemoteProfiling initializes Pyroscope profiling with OpenTelemetry integration
func (m *Monitor) initRemoteProfiling() error {
	var serverAddress string
	if m.config.PyroscopeURL != "" {
		serverAddress = m.config.PyroscopeURL
	} else {
		// Default to using the same OTLP endpoint host with Pyroscope port
		serverAddress = "http://localhost:4040"
	}

	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: m.config.ServiceName,
		ServerAddress:   serverAddress,
		Tags: map[string]string{
			"node_name":     m.config.NodeName,
			"node_uid":      m.config.NodeUID,
			"node_location": m.config.NodeLocation,
			"version":       m.config.ServiceVersion,
			"otel_enabled":  "true",
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

	log.Info().Msgf("Monitoring: Using remote profiling with OpenTelemetry integration: %s", serverAddress)
	return nil
}

// initFileBasedProfiling initializes file-based profiling export
func (m *Monitor) initFileBasedProfiling() error {
	profilingDir := filepath.Join(m.config.FileExportPath, "profiling")
	if err := os.MkdirAll(profilingDir, 0755); err != nil {
		return fmt.Errorf("failed to create profiling directory: %w", err)
	}

	// Start periodic profiling collection to files
	go m.startPeriodicProfilingExport(profilingDir)

	log.Info().Msgf("Monitoring: Using file-based profiling export: %s", profilingDir)
	return nil
}

// startPeriodicProfilingExport starts background profiling data collection to files
func (m *Monitor) startPeriodicProfilingExport(profilingDir string) {
	ticker := time.NewTicker(time.Duration(m.config.ProfilingInterval))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.collectProfilesToFile(profilingDir)
		case <-m.shutdownCh:
			return
		}
	}
}

// collectProfilesToFile collects various profiles and writes them to files
func (m *Monitor) collectProfilesToFile(profilingDir string) {
	timestamp := time.Now().Format("20060102-150405")

	// CPU profile
	if err := m.collectCPUProfile(filepath.Join(profilingDir, fmt.Sprintf("cpu-%s.pprof", timestamp))); err != nil {
		log.Debug().Msgf("Monitoring: Failed to collect CPU profile: %v", err)
	}

	// Memory profile
	if err := m.collectMemoryProfile(filepath.Join(profilingDir, fmt.Sprintf("heap-%s.pprof", timestamp))); err != nil {
		log.Debug().Msgf("Monitoring: Failed to collect memory profile: %v", err)
	}

	// Goroutine profile
	if err := m.collectGoroutineProfile(filepath.Join(profilingDir, fmt.Sprintf("goroutine-%s.pprof", timestamp))); err != nil {
		log.Debug().Msgf("Monitoring: Failed to collect goroutine profile: %v", err)
	}
}

// collectCPUProfile collects a CPU profile
func (m *Monitor) collectCPUProfile(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := pprof.StartCPUProfile(f); err != nil {
		return err
	}

	// Collect for 10 seconds
	time.Sleep(10 * time.Second)
	pprof.StopCPUProfile()

	return nil
}

// collectMemoryProfile collects a memory profile
func (m *Monitor) collectMemoryProfile(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return pprof.WriteHeapProfile(f)
}

// collectGoroutineProfile collects a goroutine profile
func (m *Monitor) collectGoroutineProfile(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return pprof.Lookup("goroutine").WriteTo(f, 0)
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
	log.Info().Msg("Monitoring: Shutting down...")

	// Signal shutdown to background goroutines
	if m.shutdownCh != nil {
		close(m.shutdownCh)
	}

	var errs []error
	for _, shutdown := range m.shutdownFuncs {
		if err := shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	log.Info().Msg("Monitoring: Shutdown complete")
	return nil
}

// IsEnabled returns whether monitoring is enabled
func (m *Monitor) IsEnabled() bool {
	return m.config.Enabled
}

// FileTraceExporter implements a file-based trace exporter
type FileTraceExporter struct {
	file *os.File
	mu   sync.Mutex
}

// NewFileTraceExporter creates a new file-based trace exporter
func NewFileTraceExporter(filename string) (*FileTraceExporter, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace file: %w", err)
	}
	return &FileTraceExporter{file: file}, nil
}

// ExportSpans exports spans to the file
func (f *FileTraceExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, span := range spans {
		// Convert span to a simple JSON structure
		spanData := map[string]interface{}{
			"trace_id":   span.SpanContext().TraceID().String(),
			"span_id":    span.SpanContext().SpanID().String(),
			"parent_id":  span.Parent().SpanID().String(),
			"name":       span.Name(),
			"start_time": span.StartTime(),
			"end_time":   span.EndTime(),
			"duration":   span.EndTime().Sub(span.StartTime()),
			"status":     span.Status().Code.String(),
			"attributes": attributesToMap(span.Attributes()),
			"resource":   resourceToMap(span.Resource()),
		}

		data, err := json.Marshal(spanData)
		if err != nil {
			return fmt.Errorf("failed to marshal span: %w", err)
		}

		if _, err := f.file.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("failed to write span: %w", err)
		}
	}

	return nil
}

// Shutdown closes the file
func (f *FileTraceExporter) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Close()
}

// attributesToMap converts OpenTelemetry attributes to a map
func attributesToMap(attrs []attribute.KeyValue) map[string]interface{} {
	result := make(map[string]interface{})
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.AsInterface()
	}
	return result
}

// resourceToMap converts OpenTelemetry resource to a map
func resourceToMap(res *resource.Resource) map[string]interface{} {
	result := make(map[string]interface{})
	for _, attr := range res.Attributes() {
		result[string(attr.Key)] = attr.Value.AsInterface()
	}
	return result
}

// FileMetricExporter implements a file-based metric exporter
type FileMetricExporter struct {
	file *os.File
	mu   sync.Mutex
}

// NewFileMetricExporter creates a new file-based metric exporter
func NewFileMetricExporter(filename string) (*FileMetricExporter, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open metric file: %w", err)
	}
	return &FileMetricExporter{file: file}, nil
}

// WriteMetrics writes metrics data to the file
func (f *FileMetricExporter) WriteMetrics(metricsData map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	jsonData, err := json.Marshal(metricsData)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	if _, err := f.file.Write(append(jsonData, '\n')); err != nil {
		return fmt.Errorf("failed to write metrics: %w", err)
	}

	return nil
}

// Shutdown closes the file
func (f *FileMetricExporter) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Close()
}

// startPeriodicMetricsFileExport starts a goroutine that periodically exports metrics to a file
func (m *Monitor) startPeriodicMetricsFileExport(metricsFile string) {
	ticker := time.NewTicker(time.Duration(m.config.MetricsInterval))
	defer ticker.Stop()

	fileExporter, err := NewFileMetricExporter(metricsFile)
	if err != nil {
		log.Error().Msgf("Monitoring: Failed to create metrics file exporter: %v", err)
		return
	}
	defer fileExporter.Shutdown(context.Background())

	for {
		select {
		case <-ticker.C:
			// Collect actual metrics data from Prometheus registry
			if m.promExporter != nil {
				metricsData := m.collectPrometheusMetrics()
				if err := fileExporter.WriteMetrics(metricsData); err != nil {
					log.Debug().Msgf("Monitoring: Failed to write metrics to file: %v", err)
				}
			}
		case <-m.shutdownCh:
			return
		}
	}
}

// collectPrometheusMetrics collects metrics from the Prometheus registry
func (m *Monitor) collectPrometheusMetrics() map[string]interface{} {
	metricsData := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    "prometheus",
		"service": map[string]interface{}{
			"name":    m.config.ServiceName,
			"version": m.config.ServiceVersion,
		},
		"node": map[string]interface{}{
			"name":     m.config.NodeName,
			"uid":      m.config.NodeUID,
			"location": m.config.NodeLocation,
		},
	}

	// Try to gather metrics from Prometheus registry
	// Note: This is a simplified approach. In a production system,
	// you might want to use the Prometheus client API to get actual metric values
	if m.metrics != nil {
		// Collect system metrics
		ctx := context.Background()
		systemMetrics := m.collectCurrentSystemMetrics(ctx)
		metricsData["system"] = systemMetrics

		// Add runtime metrics
		runtimeMetrics := m.collectCurrentRuntimeMetrics()
		metricsData["runtime"] = runtimeMetrics
	}

	return metricsData
}

// collectCurrentSystemMetrics collects current system metrics
func (m *Monitor) collectCurrentSystemMetrics(ctx context.Context) map[string]interface{} {
	systemMetrics := make(map[string]interface{})

	// CPU metrics
	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		systemMetrics["cpu_usage_percent"] = cpuPercent[0]
	}

	// Memory metrics
	if memInfo, err := mem.VirtualMemory(); err == nil {
		systemMetrics["memory_usage_percent"] = memInfo.UsedPercent
		systemMetrics["memory_total_bytes"] = memInfo.Total
		systemMetrics["memory_used_bytes"] = memInfo.Used
		systemMetrics["memory_available_bytes"] = memInfo.Available
	}

	// Disk metrics
	if diskInfo, err := disk.Usage("/"); err == nil {
		systemMetrics["disk_usage_percent"] = diskInfo.UsedPercent
		systemMetrics["disk_total_bytes"] = diskInfo.Total
		systemMetrics["disk_used_bytes"] = diskInfo.Used
		systemMetrics["disk_free_bytes"] = diskInfo.Free
	}

	// Network metrics
	if netStats, err := net.IOCounters(false); err == nil && len(netStats) > 0 {
		systemMetrics["network_rx_bytes"] = netStats[0].BytesRecv
		systemMetrics["network_tx_bytes"] = netStats[0].BytesSent
		systemMetrics["network_rx_packets"] = netStats[0].PacketsRecv
		systemMetrics["network_tx_packets"] = netStats[0].PacketsSent
	}

	return systemMetrics
}

// collectCurrentRuntimeMetrics collects current Go runtime metrics
func (m *Monitor) collectCurrentRuntimeMetrics() map[string]interface{} {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return map[string]interface{}{
		"goroutines_count":    runtime.NumGoroutine(),
		"gc_cycles":           stats.NumGC,
		"heap_alloc_bytes":    stats.HeapAlloc,
		"heap_sys_bytes":      stats.HeapSys,
		"heap_idle_bytes":     stats.HeapIdle,
		"heap_inuse_bytes":    stats.HeapInuse,
		"heap_released_bytes": stats.HeapReleased,
		"heap_objects":        stats.HeapObjects,
		"stack_inuse_bytes":   stats.StackInuse,
		"stack_sys_bytes":     stats.StackSys,
		"next_gc_bytes":       stats.NextGC,
		"last_gc_time":        time.Unix(0, int64(stats.LastGC)).Format(time.RFC3339),
		"gc_pause_ns":         stats.PauseNs[(stats.NumGC+255)%256],
	}
}

// FileLogExporter implements a file-based log exporter
type FileLogExporter struct {
	file *os.File
	mu   sync.Mutex
}

// NewFileLogExporter creates a new file-based log exporter
func NewFileLogExporter(filename string) (*FileLogExporter, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	return &FileLogExporter{file: file}, nil
}

// Export exports logs to the file
func (f *FileLogExporter) Export(ctx context.Context, records []otellog.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, record := range records {
		// Convert log to a simple JSON structure
		logData := map[string]interface{}{
			"timestamp": record.Timestamp(),
			"severity":  record.Severity().String(),
			"body":      record.Body().AsString(),
		}

		data, err := json.Marshal(logData)
		if err != nil {
			return fmt.Errorf("failed to marshal log: %w", err)
		}

		if _, err := f.file.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("failed to write log: %w", err)
		}
	}

	return nil
}

// ForceFlush forces a flush of the log exporter
func (f *FileLogExporter) ForceFlush(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Sync()
}

// Shutdown closes the file
func (f *FileLogExporter) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Close()
}
