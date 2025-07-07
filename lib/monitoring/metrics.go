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

package monitoring

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	fishlog "github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// Metrics holds all monitoring metrics for Aquarium Fish
type Metrics struct {
	meter metric.Meter

	// System metrics
	cpuUsage    metric.Float64Gauge
	memoryUsage metric.Float64Gauge
	memoryTotal metric.Int64Gauge
	diskUsage   metric.Float64Gauge
	diskTotal   metric.Int64Gauge
	networkRx   metric.Int64Counter
	networkTx   metric.Int64Counter
	goroutines  metric.Int64Gauge
	gcPauses    metric.Float64Histogram

	// Application metrics
	apiRequests        metric.Int64Counter
	apiRequestDuration metric.Float64Histogram
	allocations        metric.Int64Counter
	deallocations      metric.Int64Counter
	allocationErrors   metric.Int64Counter
	electionRounds     metric.Int64Counter
	electionFailures   metric.Int64Counter

	// Certificate metrics
	certExpiry metric.Float64Gauge

	// Driver metrics
	awsPoolSize      metric.Int64Gauge
	awsPoolUsage     metric.Int64Gauge
	awsInstanceCpu   metric.Float64Gauge
	awsInstanceDisk  metric.Float64Gauge
	awsInstanceNet   metric.Int64Counter
	driverOperations metric.Int64Counter
	driverErrors     metric.Int64Counter

	// RPC metrics
	rpcChannelLoad   metric.Int64Gauge
	rpcConnections   metric.Int64Gauge
	streamingClients metric.Int64Gauge

	// Database metrics
	dbOperations  metric.Int64Counter
	dbSize        metric.Int64Gauge
	dbKeys        metric.Int64Gauge
	dbCompactions metric.Int64Counter

	// Synchronization
	mu sync.RWMutex

	// Background collection
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// New metrics
	RPCRequestDuration        metric.Float64Histogram
	RPCRequestCount           metric.Int64Counter
	RPCRequestErrors          metric.Int64Counter
	RPCActiveRequests         metric.Int64UpDownCounter
	ApplicationStateChanges   metric.Int64Counter
	ApplicationCreations      metric.Int64Counter
	ApplicationDeallocations  metric.Int64Counter
	ApplicationsByState       metric.Int64Gauge
	ApplicationProcessingTime metric.Float64Histogram
	DatabaseOperationDuration metric.Float64Histogram
	DatabaseOperationCount    metric.Int64Counter
	DatabaseErrors            metric.Int64Counter
	DatabaseActiveConnections metric.Int64UpDownCounter
	SystemCPUUsage            metric.Float64Gauge
	SystemMemoryUsage         metric.Float64Gauge
	SystemDiskUsage           metric.Float64Gauge
	SystemNetworkRxBytes      metric.Int64Counter
	SystemNetworkTxBytes      metric.Int64Counter
	GoGoroutines              metric.Int64Gauge
	GoMemoryHeap              metric.Int64Gauge
	GoMemoryStack             metric.Int64Gauge
	GoGCCycles                metric.Int64Counter
}

// NewMetrics creates a new metrics collection
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{
		meter:  meter,
		stopCh: make(chan struct{}),
	}

	var err error

	// System metrics
	if m.cpuUsage, err = meter.Float64Gauge("fish_cpu_usage_percent"); err != nil {
		return nil, fmt.Errorf("failed to create cpu_usage metric: %w", err)
	}

	if m.memoryUsage, err = meter.Float64Gauge("fish_memory_usage_percent"); err != nil {
		return nil, fmt.Errorf("failed to create memory_usage metric: %w", err)
	}

	if m.memoryTotal, err = meter.Int64Gauge("fish_memory_total_bytes"); err != nil {
		return nil, fmt.Errorf("failed to create memory_total metric: %w", err)
	}

	if m.diskUsage, err = meter.Float64Gauge("fish_disk_usage_percent"); err != nil {
		return nil, fmt.Errorf("failed to create disk_usage metric: %w", err)
	}

	if m.diskTotal, err = meter.Int64Gauge("fish_disk_total_bytes"); err != nil {
		return nil, fmt.Errorf("failed to create disk_total metric: %w", err)
	}

	if m.networkRx, err = meter.Int64Counter("fish_network_received_bytes_total"); err != nil {
		return nil, fmt.Errorf("failed to create network_rx metric: %w", err)
	}

	if m.networkTx, err = meter.Int64Counter("fish_network_transmitted_bytes_total"); err != nil {
		return nil, fmt.Errorf("failed to create network_tx metric: %w", err)
	}

	if m.goroutines, err = meter.Int64Gauge("fish_goroutines_current"); err != nil {
		return nil, fmt.Errorf("failed to create goroutines metric: %w", err)
	}

	if m.gcPauses, err = meter.Float64Histogram("fish_gc_pause_seconds"); err != nil {
		return nil, fmt.Errorf("failed to create gc_pauses metric: %w", err)
	}

	// Application metrics
	if m.apiRequests, err = meter.Int64Counter("fish_api_requests_total"); err != nil {
		return nil, fmt.Errorf("failed to create api_requests metric: %w", err)
	}

	if m.apiRequestDuration, err = meter.Float64Histogram("fish_api_request_duration_seconds"); err != nil {
		return nil, fmt.Errorf("failed to create api_request_duration metric: %w", err)
	}

	if m.allocations, err = meter.Int64Counter("fish_allocations_total"); err != nil {
		return nil, fmt.Errorf("failed to create allocations metric: %w", err)
	}

	if m.deallocations, err = meter.Int64Counter("fish_deallocations_total"); err != nil {
		return nil, fmt.Errorf("failed to create deallocations metric: %w", err)
	}

	if m.allocationErrors, err = meter.Int64Counter("fish_allocation_errors_total"); err != nil {
		return nil, fmt.Errorf("failed to create allocation_errors metric: %w", err)
	}

	if m.electionRounds, err = meter.Int64Counter("fish_election_rounds_total"); err != nil {
		return nil, fmt.Errorf("failed to create election_rounds metric: %w", err)
	}

	if m.electionFailures, err = meter.Int64Counter("fish_election_failures_total"); err != nil {
		return nil, fmt.Errorf("failed to create election_failures metric: %w", err)
	}

	// Certificate metrics
	if m.certExpiry, err = meter.Float64Gauge("fish_certificate_expiry_seconds"); err != nil {
		return nil, fmt.Errorf("failed to create cert_expiry metric: %w", err)
	}

	// Driver metrics
	if m.awsPoolSize, err = meter.Int64Gauge("fish_aws_pool_size"); err != nil {
		return nil, fmt.Errorf("failed to create aws_pool_size metric: %w", err)
	}

	if m.awsPoolUsage, err = meter.Int64Gauge("fish_aws_pool_usage"); err != nil {
		return nil, fmt.Errorf("failed to create aws_pool_usage metric: %w", err)
	}

	if m.awsInstanceCpu, err = meter.Float64Gauge("fish_aws_instance_cpu_percent"); err != nil {
		return nil, fmt.Errorf("failed to create aws_instance_cpu metric: %w", err)
	}

	if m.awsInstanceDisk, err = meter.Float64Gauge("fish_aws_instance_disk_percent"); err != nil {
		return nil, fmt.Errorf("failed to create aws_instance_disk metric: %w", err)
	}

	if m.awsInstanceNet, err = meter.Int64Counter("fish_aws_instance_network_bytes_total"); err != nil {
		return nil, fmt.Errorf("failed to create aws_instance_net metric: %w", err)
	}

	if m.driverOperations, err = meter.Int64Counter("fish_driver_operations_total"); err != nil {
		return nil, fmt.Errorf("failed to create driver_operations metric: %w", err)
	}

	if m.driverErrors, err = meter.Int64Counter("fish_driver_errors_total"); err != nil {
		return nil, fmt.Errorf("failed to create driver_errors metric: %w", err)
	}

	// RPC metrics
	if m.rpcChannelLoad, err = meter.Int64Gauge("fish_rpc_channel_load"); err != nil {
		return nil, fmt.Errorf("failed to create rpc_channel_load metric: %w", err)
	}

	if m.rpcConnections, err = meter.Int64Gauge("fish_rpc_connections_current"); err != nil {
		return nil, fmt.Errorf("failed to create rpc_connections metric: %w", err)
	}

	if m.streamingClients, err = meter.Int64Gauge("fish_streaming_clients_current"); err != nil {
		return nil, fmt.Errorf("failed to create streaming_clients metric: %w", err)
	}

	// Database metrics
	if m.dbOperations, err = meter.Int64Counter("fish_db_operations_total"); err != nil {
		return nil, fmt.Errorf("failed to create db_operations metric: %w", err)
	}

	if m.dbSize, err = meter.Int64Gauge("fish_db_size_bytes"); err != nil {
		return nil, fmt.Errorf("failed to create db_size metric: %w", err)
	}

	if m.dbKeys, err = meter.Int64Gauge("fish_db_keys_total"); err != nil {
		return nil, fmt.Errorf("failed to create db_keys metric: %w", err)
	}

	if m.dbCompactions, err = meter.Int64Counter("fish_db_compactions_total"); err != nil {
		return nil, fmt.Errorf("failed to create db_compactions metric: %w", err)
	}

	// New metrics
	m.RPCRequestDuration, err = meter.Float64Histogram(
		"aquarium_rpc_request_duration_seconds",
		metric.WithDescription("Duration of RPC requests in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPCRequestDuration metric: %w", err)
	}

	m.RPCRequestCount, err = meter.Int64Counter(
		"aquarium_rpc_requests_total",
		metric.WithDescription("Total number of RPC requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPCRequestCount metric: %w", err)
	}

	m.RPCRequestErrors, err = meter.Int64Counter(
		"aquarium_rpc_request_errors_total",
		metric.WithDescription("Total number of RPC request errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPCRequestErrors metric: %w", err)
	}

	m.RPCActiveRequests, err = meter.Int64UpDownCounter(
		"aquarium_rpc_active_requests",
		metric.WithDescription("Number of active RPC requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPCActiveRequests metric: %w", err)
	}

	m.ApplicationStateChanges, err = meter.Int64Counter(
		"aquarium_application_state_changes_total",
		metric.WithDescription("Total number of application state changes"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ApplicationStateChanges metric: %w", err)
	}

	m.ApplicationCreations, err = meter.Int64Counter(
		"aquarium_application_creations_total",
		metric.WithDescription("Total number of applications created"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ApplicationCreations metric: %w", err)
	}

	m.ApplicationDeallocations, err = meter.Int64Counter(
		"aquarium_application_deallocations_total",
		metric.WithDescription("Total number of applications deallocated"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ApplicationDeallocations metric: %w", err)
	}

	m.ApplicationsByState, err = meter.Int64Gauge(
		"aquarium_applications_by_state",
		metric.WithDescription("Number of applications by state"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ApplicationsByState metric: %w", err)
	}

	m.ApplicationProcessingTime, err = meter.Float64Histogram(
		"aquarium_application_processing_time_seconds",
		metric.WithDescription("Time taken to process applications through states"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ApplicationProcessingTime metric: %w", err)
	}

	m.DatabaseOperationDuration, err = meter.Float64Histogram(
		"aquarium_database_operation_duration_seconds",
		metric.WithDescription("Duration of database operations in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DatabaseOperationDuration metric: %w", err)
	}

	m.DatabaseOperationCount, err = meter.Int64Counter(
		"aquarium_database_operations_total",
		metric.WithDescription("Total number of database operations"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DatabaseOperationCount metric: %w", err)
	}

	m.DatabaseErrors, err = meter.Int64Counter(
		"aquarium_database_errors_total",
		metric.WithDescription("Total number of database errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DatabaseErrors metric: %w", err)
	}

	m.DatabaseActiveConnections, err = meter.Int64UpDownCounter(
		"aquarium_database_active_connections",
		metric.WithDescription("Number of active database connections"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DatabaseActiveConnections metric: %w", err)
	}

	m.SystemCPUUsage, err = meter.Float64Gauge(
		"aquarium_system_cpu_usage_percent",
		metric.WithDescription("System CPU usage percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SystemCPUUsage metric: %w", err)
	}

	m.SystemMemoryUsage, err = meter.Float64Gauge(
		"aquarium_system_memory_usage_percent",
		metric.WithDescription("System memory usage percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SystemMemoryUsage metric: %w", err)
	}

	m.SystemDiskUsage, err = meter.Float64Gauge(
		"aquarium_system_disk_usage_percent",
		metric.WithDescription("System disk usage percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SystemDiskUsage metric: %w", err)
	}

	m.SystemNetworkRxBytes, err = meter.Int64Counter(
		"aquarium_system_network_rx_bytes_total",
		metric.WithDescription("Total network bytes received"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SystemNetworkRxBytes metric: %w", err)
	}

	m.SystemNetworkTxBytes, err = meter.Int64Counter(
		"aquarium_system_network_tx_bytes_total",
		metric.WithDescription("Total network bytes transmitted"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SystemNetworkTxBytes metric: %w", err)
	}

	m.GoGoroutines, err = meter.Int64Gauge(
		"aquarium_go_goroutines",
		metric.WithDescription("Number of goroutines"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GoGoroutines metric: %w", err)
	}

	m.GoMemoryHeap, err = meter.Int64Gauge(
		"aquarium_go_memory_heap_bytes",
		metric.WithDescription("Go heap memory in bytes"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GoMemoryHeap metric: %w", err)
	}

	m.GoMemoryStack, err = meter.Int64Gauge(
		"aquarium_go_memory_stack_bytes",
		metric.WithDescription("Go stack memory in bytes"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GoMemoryStack metric: %w", err)
	}

	m.GoGCCycles, err = meter.Int64Counter(
		"aquarium_go_gc_cycles_total",
		metric.WithDescription("Total number of Go GC cycles"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GoGCCycles metric: %w", err)
	}

	return m, nil
}

// StartCollection starts automatic metrics collection
func (m *Metrics) StartCollection(ctx context.Context, interval time.Duration) {
	m.wg.Add(1)
	go m.collectLoop(ctx, interval)
}

// StopCollection stops automatic metrics collection
func (m *Metrics) StopCollection() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		m.wg.Wait()
	})
}

// collectLoop runs the metrics collection loop
func (m *Metrics) collectLoop(ctx context.Context, interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.collectSystemMetrics(ctx)
			m.collectRuntimeMetrics(ctx)
		}
	}
}

// collectSystemMetrics collects system-level metrics
func (m *Metrics) collectSystemMetrics(ctx context.Context) {
	// CPU metrics
	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		m.cpuUsage.Record(ctx, cpuPercent[0])
	}

	// Memory metrics
	if memInfo, err := mem.VirtualMemory(); err == nil {
		m.memoryUsage.Record(ctx, memInfo.UsedPercent)
		m.memoryTotal.Record(ctx, int64(memInfo.Total))
	}

	// Disk metrics
	if diskInfo, err := disk.Usage("/"); err == nil {
		m.diskUsage.Record(ctx, diskInfo.UsedPercent)
		m.diskTotal.Record(ctx, int64(diskInfo.Total))
	}

	// Network metrics
	if netStats, err := net.IOCounters(false); err == nil && len(netStats) > 0 {
		m.networkRx.Add(ctx, int64(netStats[0].BytesRecv))
		m.networkTx.Add(ctx, int64(netStats[0].BytesSent))
	}
}

// collectRuntimeMetrics collects Go runtime metrics
func (m *Metrics) collectRuntimeMetrics(ctx context.Context) {
	// Goroutines
	m.goroutines.Record(ctx, int64(runtime.NumGoroutine()))

	// GC metrics
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	// Convert nanoseconds to seconds
	gcPauseSeconds := float64(stats.PauseNs[(stats.NumGC+255)%256]) / 1e9
	m.gcPauses.Record(ctx, gcPauseSeconds)
}

// RecordAPIRequest records an API request
func (m *Metrics) RecordAPIRequest(ctx context.Context, method, endpoint, status string, duration time.Duration) {
	attrs := []attribute.KeyValue{
		attribute.String("method", method),
		attribute.String("endpoint", endpoint),
		attribute.String("status", status),
	}

	m.apiRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.apiRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// RecordAllocation records an application allocation
func (m *Metrics) RecordAllocation(ctx context.Context, driver, status string) {
	attrs := []attribute.KeyValue{
		attribute.String("driver", driver),
		attribute.String("status", status),
	}

	if status == "success" {
		m.allocations.Add(ctx, 1, metric.WithAttributes(attrs...))
	} else {
		m.allocationErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordDeallocation records an application deallocation
func (m *Metrics) RecordDeallocation(ctx context.Context, driver string) {
	attrs := []attribute.KeyValue{
		attribute.String("driver", driver),
	}

	m.deallocations.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordElectionRound records an election round
func (m *Metrics) RecordElectionRound(ctx context.Context, outcome string) {
	attrs := []attribute.KeyValue{
		attribute.String("outcome", outcome),
	}

	m.electionRounds.Add(ctx, 1, metric.WithAttributes(attrs...))

	if outcome == "failed" {
		m.electionFailures.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// UpdateCertificateExpiry updates certificate expiry metrics
func (m *Metrics) UpdateCertificateExpiry(ctx context.Context, certType, certPath string) {
	expiry, err := m.getCertificateExpiry(certPath)
	if err != nil {
		fishlog.Debug().Msgf("Monitoring: Failed to get certificate expiry for %s: %v", certPath, err)
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("cert_type", certType),
		attribute.String("cert_path", certPath),
	}

	secondsUntilExpiry := time.Until(expiry).Seconds()
	m.certExpiry.Record(ctx, secondsUntilExpiry, metric.WithAttributes(attrs...))
}

// getCertificateExpiry extracts expiry time from a PEM certificate file
func (m *Metrics) getCertificateExpiry(certPath string) (time.Time, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}

	return cert.NotAfter, nil
}

// UpdateAWSPoolMetrics updates AWS pool metrics
func (m *Metrics) UpdateAWSPoolMetrics(ctx context.Context, poolName string, size, usage int64) {
	attrs := []attribute.KeyValue{
		attribute.String("pool_name", poolName),
	}

	m.awsPoolSize.Record(ctx, size, metric.WithAttributes(attrs...))
	m.awsPoolUsage.Record(ctx, usage, metric.WithAttributes(attrs...))
}

// UpdateAWSInstanceMetrics updates AWS instance metrics
func (m *Metrics) UpdateAWSInstanceMetrics(ctx context.Context, instanceId string, cpuPercent, diskPercent float64, networkBytes int64) {
	attrs := []attribute.KeyValue{
		attribute.String("instance_id", instanceId),
	}

	m.awsInstanceCpu.Record(ctx, cpuPercent, metric.WithAttributes(attrs...))
	m.awsInstanceDisk.Record(ctx, diskPercent, metric.WithAttributes(attrs...))
	m.awsInstanceNet.Add(ctx, networkBytes, metric.WithAttributes(attrs...))
}

// RecordDriverOperation records a driver operation
func (m *Metrics) RecordDriverOperation(ctx context.Context, driver, operation, status string) {
	attrs := []attribute.KeyValue{
		attribute.String("driver", driver),
		attribute.String("operation", operation),
		attribute.String("status", status),
	}

	m.driverOperations.Add(ctx, 1, metric.WithAttributes(attrs...))

	if status == "error" {
		m.driverErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// UpdateRPCChannelLoad updates RPC channel load metrics
func (m *Metrics) UpdateRPCChannelLoad(ctx context.Context, channelName string, load int64) {
	attrs := []attribute.KeyValue{
		attribute.String("channel", channelName),
	}

	m.rpcChannelLoad.Record(ctx, load, metric.WithAttributes(attrs...))
}

// UpdateRPCConnections updates RPC connection count
func (m *Metrics) UpdateRPCConnections(ctx context.Context, connections int64) {
	m.rpcConnections.Record(ctx, connections)
}

// UpdateStreamingClients updates streaming client count
func (m *Metrics) UpdateStreamingClients(ctx context.Context, clients int64) {
	m.streamingClients.Record(ctx, clients)
}

// RecordDatabaseOperation records a database operation
func (m *Metrics) RecordDatabaseOperation(ctx context.Context, operation, status string, duration time.Duration) {
	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("status", status),
	}

	m.dbOperations.Add(ctx, 1, metric.WithAttributes(attrs...))

	// Record duration if available
	if m.DatabaseOperationDuration != nil && duration > 0 {
		m.DatabaseOperationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}

	// Record operation count if available
	if m.DatabaseOperationCount != nil {
		success := status == "success"
		successAttr := attribute.Bool("success", success)
		m.DatabaseOperationCount.Add(ctx, 1, metric.WithAttributes(append(attrs, successAttr)...))

		// Record errors if unsuccessful
		if m.DatabaseErrors != nil && !success {
			m.DatabaseErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
	}
}

// UpdateDatabaseStats updates database statistics
func (m *Metrics) UpdateDatabaseStats(ctx context.Context, size, keys int64) {
	m.dbSize.Record(ctx, size)
	m.dbKeys.Record(ctx, keys)
}

// RecordDatabaseCompaction records a database compaction
func (m *Metrics) RecordDatabaseCompaction(ctx context.Context) {
	m.dbCompactions.Add(ctx, 1)
}

// UpdateCertificateDirectory scans a directory for certificates and updates expiry metrics
func (m *Metrics) UpdateCertificateDirectory(ctx context.Context, certDir string) {
	err := filepath.WalkDir(certDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Check for certificate files
		if filepath.Ext(path) == ".crt" || filepath.Ext(path) == ".pem" {
			certType := "unknown"
			if filepath.Base(path) == "ca.crt" {
				certType = "ca"
			} else if filepath.Base(path) == "ca.pem" {
				certType = "ca"
			} else {
				certType = "node"
			}

			m.UpdateCertificateExpiry(ctx, certType, path)
		}

		return nil
	})

	if err != nil {
		fishlog.Debug().Msgf("Monitoring: Failed to scan certificate directory %s: %v", certDir, err)
	}
}

// RecordRPCRequest records metrics for an RPC request
func (m *Metrics) RecordRPCRequest(ctx context.Context, method string, duration time.Duration, success bool, userID string) {
	attrs := []attribute.KeyValue{
		attribute.String("method", method),
		attribute.String("user_id", userID),
	}

	// Record duration
	if m.RPCRequestDuration != nil {
		m.RPCRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}

	// Record request count
	if m.RPCRequestCount != nil {
		successAttr := attribute.Bool("success", success)
		m.RPCRequestCount.Add(ctx, 1, metric.WithAttributes(append(attrs, successAttr)...))
	}

	// Record errors if unsuccessful
	if m.RPCRequestErrors != nil && !success {
		m.RPCRequestErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordRPCActiveRequest tracks active requests
func (m *Metrics) RecordRPCActiveRequest(ctx context.Context, method string, delta int64) {
	if m.RPCActiveRequests != nil {
		attrs := []attribute.KeyValue{
			attribute.String("method", method),
		}
		m.RPCActiveRequests.Add(ctx, delta, metric.WithAttributes(attrs...))
	}
}

// RecordApplicationStateChange records application state changes
func (m *Metrics) RecordApplicationStateChange(ctx context.Context, appUID string, fromState, toState typesv2.ApplicationState_Status, userID string) {
	if m.ApplicationStateChanges != nil {
		attrs := []attribute.KeyValue{
			attribute.String("application_uid", appUID),
			attribute.String("from_state", fromState.String()),
			attribute.String("to_state", toState.String()),
			attribute.String("user_id", userID),
		}
		m.ApplicationStateChanges.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordApplicationCreation records application creation
func (m *Metrics) RecordApplicationCreation(ctx context.Context, appUID, labelUID, userID string) {
	if m.ApplicationCreations != nil {
		attrs := []attribute.KeyValue{
			attribute.String("application_uid", appUID),
			attribute.String("label_uid", labelUID),
			attribute.String("user_id", userID),
		}
		m.ApplicationCreations.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordApplicationDeallocation records application deallocation
func (m *Metrics) RecordApplicationDeallocation(ctx context.Context, appUID, userID string) {
	if m.ApplicationDeallocations != nil {
		attrs := []attribute.KeyValue{
			attribute.String("application_uid", appUID),
			attribute.String("user_id", userID),
		}
		m.ApplicationDeallocations.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordApplicationsByState records the current count of applications by state
func (m *Metrics) RecordApplicationsByState(ctx context.Context, state typesv2.ApplicationState_Status, count int64) {
	if m.ApplicationsByState != nil {
		attrs := []attribute.KeyValue{
			attribute.String("state", state.String()),
		}
		m.ApplicationsByState.Record(ctx, count, metric.WithAttributes(attrs...))
	}
}

// RecordApplicationProcessingTime records how long it takes to process applications through states
func (m *Metrics) RecordApplicationProcessingTime(ctx context.Context, appUID string, fromState, toState typesv2.ApplicationState_Status, duration time.Duration) {
	if m.ApplicationProcessingTime != nil {
		attrs := []attribute.KeyValue{
			attribute.String("application_uid", appUID),
			attribute.String("from_state", fromState.String()),
			attribute.String("to_state", toState.String()),
		}
		m.ApplicationProcessingTime.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// RecordDatabaseActiveConnections tracks active database connections
func (m *Metrics) RecordDatabaseActiveConnections(ctx context.Context, delta int64) {
	if m.DatabaseActiveConnections != nil {
		m.DatabaseActiveConnections.Add(ctx, delta)
	}
}

// CollectSystemMetrics collects and records system metrics
func (m *Metrics) CollectSystemMetrics(ctx context.Context) {
	// CPU metrics
	if m.SystemCPUUsage != nil {
		if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
			m.SystemCPUUsage.Record(ctx, cpuPercent[0])
		}
	}

	// Memory metrics
	if m.SystemMemoryUsage != nil {
		if memInfo, err := mem.VirtualMemory(); err == nil {
			m.SystemMemoryUsage.Record(ctx, memInfo.UsedPercent)
		}
	}

	// Disk metrics
	if m.SystemDiskUsage != nil {
		if diskInfo, err := disk.Usage("/"); err == nil {
			m.SystemDiskUsage.Record(ctx, diskInfo.UsedPercent)
		}
	}

	// Network metrics
	if m.SystemNetworkRxBytes != nil && m.SystemNetworkTxBytes != nil {
		if netStats, err := net.IOCounters(false); err == nil && len(netStats) > 0 {
			m.SystemNetworkRxBytes.Add(ctx, int64(netStats[0].BytesRecv))
			m.SystemNetworkTxBytes.Add(ctx, int64(netStats[0].BytesSent))
		}
	}
}

// CollectRuntimeMetrics collects and records Go runtime metrics
func (m *Metrics) CollectRuntimeMetrics(ctx context.Context) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	// Goroutines
	if m.GoGoroutines != nil {
		m.GoGoroutines.Record(ctx, int64(runtime.NumGoroutine()))
	}

	// Memory metrics
	if m.GoMemoryHeap != nil {
		m.GoMemoryHeap.Record(ctx, int64(stats.HeapAlloc))
	}
	if m.GoMemoryStack != nil {
		m.GoMemoryStack.Record(ctx, int64(stats.StackInuse))
	}

	// GC metrics
	if m.GoGCCycles != nil {
		m.GoGCCycles.Add(ctx, int64(stats.NumGC))
	}
}
