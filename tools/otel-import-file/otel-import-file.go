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

// otel-import-file tool for importing OpenTelemetry data from files to remote endpoints
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds the configuration for the import tool
type Config struct {
	OtelDir       string
	OTLPEndpoint  string
	PyroscopeURL  string
	BatchSize     int
	FlushInterval time.Duration
}

// TelemetryData represents the structure of our telemetry data
type TelemetryData struct {
	TraceID    string                 `json:"trace_id"`
	SpanID     string                 `json:"span_id"`
	ParentID   string                 `json:"parent_id"`
	Name       string                 `json:"name"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	Duration   time.Duration          `json:"duration"`
	Status     string                 `json:"status"`
	Attributes map[string]interface{} `json:"attributes"`
	Resource   map[string]interface{} `json:"resource"`
}

// LogData represents the structure of our log data
type LogData struct {
	Timestamp time.Time `json:"timestamp"`
	Severity  string    `json:"severity"`
	Body      string    `json:"body"`
}

// MetricData represents the structure of our metric data
type MetricData struct {
	Timestamp time.Time              `json:"timestamp"`
	Resource  map[string]interface{} `json:"resource"`
	Metrics   interface{}            `json:"metrics"`
}

// Importer handles the import of telemetry data
type Importer struct {
	config         Config
	httpClient     *http.Client
	traceExporter  trace.SpanExporter
	metricExporter metric.Exporter
	logExporter    log.Exporter
	grpcConn       *grpc.ClientConn
}

// NewImporter creates a new importer instance
func NewImporter(config Config) (*Importer, error) {
	importer := &Importer{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	// Setup OTLP clients if endpoint is provided
	if config.OTLPEndpoint != "" {
		if err := importer.setupOTLPClients(); err != nil {
			return nil, fmt.Errorf("failed to setup OTLP clients: %w", err)
		}
	}

	return importer, nil
}

// setupOTLPClients initializes the OTLP clients
func (i *Importer) setupOTLPClients() error {
	var err error

	// Create gRPC connection
	i.grpcConn, err = grpc.NewClient(i.config.OTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	// Create trace exporter
	i.traceExporter, err = otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithGRPCConn(i.grpcConn))
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create metrics exporter
	i.metricExporter, err = otlpmetricgrpc.New(context.Background(),
		otlpmetricgrpc.WithGRPCConn(i.grpcConn))
	if err != nil {
		return fmt.Errorf("failed to create metrics exporter: %w", err)
	}

	// Create log exporter
	i.logExporter, err = otlploggrpc.New(context.Background(),
		otlploggrpc.WithGRPCConn(i.grpcConn))
	if err != nil {
		return fmt.Errorf("failed to create log exporter: %w", err)
	}

	fmt.Printf("Connected to OTLP endpoint: %s\n", i.config.OTLPEndpoint)
	return nil
}

// Close closes all the connections and exporters
func (i *Importer) Close() error {
	var errs []error

	if i.traceExporter != nil {
		if err := i.traceExporter.Shutdown(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}

	if i.metricExporter != nil {
		if err := i.metricExporter.Shutdown(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}

	if i.logExporter != nil {
		if err := i.logExporter.Shutdown(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}

	if i.grpcConn != nil {
		if err := i.grpcConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing importer: %v", errs)
	}

	return nil
}

// ImportAll imports all telemetry data from the specified directory
func (i *Importer) ImportAll(ctx context.Context) error {
	fmt.Printf("Starting import from directory: %s\n", i.config.OtelDir)

	// Import traces
	tracesDir := filepath.Join(i.config.OtelDir, "traces")
	if err := i.importTraces(ctx, tracesDir); err != nil {
		return fmt.Errorf("failed to import traces: %w", err)
	}

	// Import metrics
	metricsDir := filepath.Join(i.config.OtelDir, "metrics")
	if err := i.importMetrics(ctx, metricsDir); err != nil {
		return fmt.Errorf("failed to import metrics: %w", err)
	}

	// Import logs
	logsDir := filepath.Join(i.config.OtelDir, "logs")
	if err := i.importLogs(ctx, logsDir); err != nil {
		return fmt.Errorf("failed to import logs: %w", err)
	}

	// Import profiling data
	profilingDir := filepath.Join(i.config.OtelDir, "profiling")
	if err := i.importProfiling(ctx, profilingDir); err != nil {
		return fmt.Errorf("failed to import profiling: %w", err)
	}

	fmt.Println("Import completed successfully!")
	return nil
}

// importTraces imports trace data from the traces directory
func (i *Importer) importTraces(ctx context.Context, tracesDir string) error {
	fmt.Printf("Importing traces from: %s\n", tracesDir)

	files, err := os.ReadDir(tracesDir)
	if err != nil {
		return fmt.Errorf("failed to read traces directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}

		filePath := filepath.Join(tracesDir, file.Name())
		fmt.Printf("Processing trace file: %s\n", filePath)

		if err := i.processTraceFile(ctx, filePath); err != nil {
			fmt.Printf("Warning: Failed to process trace file %s: %v\n", filePath, err)
		}
	}

	return nil
}

// importMetrics imports metric data from the metrics directory
func (i *Importer) importMetrics(ctx context.Context, metricsDir string) error {
	fmt.Printf("Importing metrics from: %s\n", metricsDir)

	files, err := os.ReadDir(metricsDir)
	if err != nil {
		return fmt.Errorf("failed to read metrics directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}

		filePath := filepath.Join(metricsDir, file.Name())
		fmt.Printf("Processing metric file: %s\n", filePath)

		if err := i.processMetricFile(ctx, filePath); err != nil {
			fmt.Printf("Warning: Failed to process metric file %s: %v\n", filePath, err)
		}
	}

	return nil
}

// importLogs imports log data from the logs directory
func (i *Importer) importLogs(ctx context.Context, logsDir string) error {
	fmt.Printf("Importing logs from: %s\n", logsDir)

	files, err := os.ReadDir(logsDir)
	if err != nil {
		return fmt.Errorf("failed to read logs directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}

		filePath := filepath.Join(logsDir, file.Name())
		fmt.Printf("Processing log file: %s\n", filePath)

		if err := i.processLogFile(ctx, filePath); err != nil {
			fmt.Printf("Warning: Failed to process log file %s: %v\n", filePath, err)
		}
	}

	return nil
}

// importProfiling imports profiling data from the profiling directory
func (i *Importer) importProfiling(ctx context.Context, profilingDir string) error {
	fmt.Printf("Importing profiling data from: %s\n", profilingDir)

	files, err := os.ReadDir(profilingDir)
	if err != nil {
		fmt.Printf("Warning: Could not read profiling directory %s: %v\n", profilingDir, err)
		return nil // Don't fail if profiling directory doesn't exist
	}

	pprofFiles := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Handle .pprof files
		if strings.HasSuffix(file.Name(), ".pprof") {
			pprofFiles++
			filePath := filepath.Join(profilingDir, file.Name())
			fmt.Printf("Found profiling file: %s\n", filePath)

			if err := i.processProfilingFile(ctx, filePath); err != nil {
				fmt.Printf("Warning: Failed to process profiling file %s: %v\n", filePath, err)
			}
		}
	}

	if pprofFiles == 0 {
		fmt.Printf("No profiling files found in %s\n", profilingDir)
	} else {
		fmt.Printf("Processed %d profiling files\n", pprofFiles)
	}

	return nil
}

// processProfilingFile processes a single profiling file
func (i *Importer) processProfilingFile(ctx context.Context, filePath string) error {
	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat profiling file: %w", err)
	}

	// Send to Pyroscope if configured
	if i.config.PyroscopeURL != "" {
		fmt.Printf("Would upload profiling file %s to Pyroscope (%s): %d bytes\n",
			filepath.Base(filePath), i.config.PyroscopeURL, fileInfo.Size())

		// In a real implementation, you would read the .pprof file and upload it to Pyroscope
		// This would involve parsing the file type from the filename and using the Pyroscope ingestion API
	} else {
		fmt.Printf("Profiling file %s ready for upload: %d bytes\n",
			filepath.Base(filePath), fileInfo.Size())
	}

	return nil
}

// processTraceFile processes a single trace file
func (i *Importer) processTraceFile(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open trace file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var traceData TelemetryData
		if err := json.Unmarshal([]byte(line), &traceData); err != nil {
			fmt.Printf("Warning: Failed to parse trace line %d: %v\n", lineCount, err)
			continue
		}

		// For now, just log that we would send the trace data
		// In a real implementation, you would convert to OTLP format and send
		fmt.Printf("Would send trace: %s (span: %s)\n", traceData.TraceID, traceData.SpanID)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading trace file: %w", err)
	}

	fmt.Printf("Processed %d trace lines from %s\n", lineCount, filePath)
	return nil
}

// processMetricFile processes a single metric file
func (i *Importer) processMetricFile(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open metric file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var metricData MetricData
		if err := json.Unmarshal([]byte(line), &metricData); err != nil {
			fmt.Printf("Warning: Failed to parse metric line %d: %v\n", lineCount, err)
			continue
		}

		// For now, just log that we would send the metric data
		fmt.Printf("Would send metric data from: %s\n", metricData.Timestamp.Format(time.RFC3339))
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading metric file: %w", err)
	}

	fmt.Printf("Processed %d metric lines from %s\n", lineCount, filePath)
	return nil
}

// processLogFile processes a single log file
func (i *Importer) processLogFile(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var logData LogData
		if err := json.Unmarshal([]byte(line), &logData); err != nil {
			fmt.Printf("Warning: Failed to parse log line %d: %v\n", lineCount, err)
			continue
		}

		// For now, just log that we would send the log data
		fmt.Printf("Would send log: %s [%s] %s\n", logData.Timestamp.Format(time.RFC3339), logData.Severity, logData.Body)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log file: %w", err)
	}

	fmt.Printf("Processed %d log lines from %s\n", lineCount, filePath)
	return nil
}

// sendToPyroscope sends profiling data to Pyroscope (placeholder)
func (i *Importer) sendToPyroscope(ctx context.Context, data interface{}) error {
	if i.config.PyroscopeURL == "" {
		return nil
	}

	fmt.Printf("Would send profiling data to Pyroscope: %s\n", i.config.PyroscopeURL)
	return nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <OTEL_DIR_PATH> <REMOTE_ADDRESS> [PYROSCOPE_URL]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  OTEL_DIR_PATH: Path to directory containing otel data (traces/, metrics/, logs/)\n")
		fmt.Fprintf(os.Stderr, "  REMOTE_ADDRESS: OTLP endpoint address (e.g., localhost:4317)\n")
		fmt.Fprintf(os.Stderr, "  PYROSCOPE_URL: Optional Pyroscope URL for profiling data\n")
		os.Exit(1)
	}

	otelDir := os.Args[1]
	remoteAddr := os.Args[2]
	var pyroscopeURL string
	if len(os.Args) > 3 {
		pyroscopeURL = os.Args[3]
	}

	// Validate otel directory exists
	if _, err := os.Stat(otelDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: OpenTelemetry directory does not exist: %s\n", otelDir)
		os.Exit(1)
	}

	// Create config
	config := Config{
		OtelDir:       otelDir,
		OTLPEndpoint:  remoteAddr,
		PyroscopeURL:  pyroscopeURL,
		BatchSize:     100,
		FlushInterval: 5 * time.Second,
	}

	// Create importer
	importer, err := NewImporter(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating importer: %v\n", err)
		os.Exit(1)
	}
	defer importer.Close()

	// Import all data
	ctx := context.Background()
	if err := importer.ImportAll(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error importing data: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Import completed successfully!")
}
