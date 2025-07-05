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
func (m *Metrics) RecordDatabaseOperation(ctx context.Context, operation, status string) {
	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("status", status),
	}

	m.dbOperations.Add(ctx, 1, metric.WithAttributes(attrs...))
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
