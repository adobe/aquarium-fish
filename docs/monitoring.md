# OpenTelemetry Monitoring for Aquarium Fish

This document describes the comprehensive OpenTelemetry monitoring system integrated into Aquarium Fish, providing observability through metrics, traces, logs, and profiling.

## Overview

The monitoring system provides:

- **Metrics Collection**: System metrics (CPU, memory, disk), application metrics (API requests, allocations, elections), and custom business metrics
- **Distributed Tracing**: Request tracing across RPC calls, database operations, and driver interactions
- **Continuous Profiling**: CPU, memory, and goroutine profiling via Pyroscope
- **Logs Integration**: Structured logging with trace correlation (temporarily disabled pending OpenTelemetry log stability)

## Quick Start

### 1. Start the Monitoring Stack

Use the Grafana OTEL LGTM container that provides all monitoring backends:

```bash
# Start the complete monitoring stack
docker run -p 3000:3000 -p 4317:4317 -p 4318:4318 -p 9090:9090 -p 4040:4040 \
  --name otel-lgtm grafana/otel-lgtm:latest
```

This provides:
- **Grafana**: http://localhost:3000 (admin/admin)
- **OTLP Endpoints**: localhost:4317 (gRPC), localhost:4318 (HTTP)
- **Prometheus**: http://localhost:9090
- **Pyroscope**: http://localhost:4040

### 2. Configure Fish with Monitoring

Create a configuration file with monitoring enabled:

```yaml
# fish-monitoring.yml
monitoring:
  enabled: true
  otlp_endpoint: "localhost:4317"
  pyroscope_url: "http://localhost:4040"
  service_name: "aquarium-fish"
  enable_tracing: true
  enable_metrics: true
  enable_profiling: true
  sample_rate: 1.0  # 100% sampling for development
  metrics_interval: "15s"

# ... rest of your Fish configuration
```

### 3. Start Fish with Monitoring

```bash
./aquarium-fish --cfg fish-monitoring.yml
```

### 4. Access Monitoring Data

- **Grafana Dashboards**: http://localhost:3000
- **Traces**: Navigate to "Explore" → "Tempo" in Grafana
- **Metrics**: Check Prometheus data source in Grafana
- **Profiling**: http://localhost:4040 for continuous profiling data

## Configuration Reference

### Monitoring Configuration

```yaml
monitoring:
  enabled: true                      # Enable/disable monitoring
  otlp_endpoint: "localhost:4317"    # OTLP gRPC endpoint
  pyroscope_url: "http://localhost:4040"  # Pyroscope profiling endpoint

  # Service identification
  service_name: "aquarium-fish"      # Service name for telemetry
  service_version: "1.0.0"           # Service version (auto-detected from build)
  node_name: "fish-node-1"           # Node name (auto-detected from config)
  node_location: "datacenter-1"      # Node location (auto-detected from config)

  # Sampling and collection
  sample_rate: 1.0                   # Trace sampling rate (0.0 to 1.0)
  metrics_interval: "15s"            # Metrics collection interval

  # Feature toggles
  enable_tracing: true               # Enable distributed tracing
  enable_metrics: true               # Enable metrics collection
  enable_logs: false                 # Enable log export (disabled pending stability)
  enable_profiling: true             # Enable continuous profiling
```

## Metrics Reference

### System Metrics

- `aquarium_fish_cpu_usage_percent`: CPU utilization percentage
- `aquarium_fish_memory_usage_bytes`: Memory usage in bytes
- `aquarium_fish_memory_usage_percent`: Memory utilization percentage
- `aquarium_fish_disk_usage_bytes`: Disk usage by mount point
- `aquarium_fish_disk_usage_percent`: Disk utilization percentage
- `aquarium_fish_network_bytes_sent`: Network bytes sent by interface
- `aquarium_fish_network_bytes_recv`: Network bytes received by interface
- `aquarium_fish_goroutines_total`: Number of active goroutines
- `aquarium_fish_gc_pause_seconds`: Garbage collection pause duration

### Application Metrics

- `aquarium_fish_api_requests_total`: Total API requests by method and endpoint
- `aquarium_fish_api_request_duration_seconds`: API request duration
- `aquarium_fish_allocations_total`: Total resource allocations
- `aquarium_fish_deallocations_total`: Total resource deallocations
- `aquarium_fish_allocation_errors_total`: Total allocation errors
- `aquarium_fish_election_rounds_total`: Total election rounds
- `aquarium_fish_election_failures_total`: Total election failures

### Certificate Metrics

- `aquarium_fish_cert_expiry_seconds`: Certificate expiration time
- `aquarium_fish_ca_cert_expiry_seconds`: CA certificate expiration time

### Database Metrics

- `aquarium_fish_db_operation_duration_seconds`: Database operation duration
- `aquarium_fish_db_operations_total`: Total database operations
- `aquarium_fish_db_size_bytes`: Database size in bytes
- `aquarium_fish_db_keys_total`: Total database keys
- `aquarium_fish_db_reclaimable_bytes`: Reclaimable database space

### AWS Driver Metrics (when enabled)

- `aquarium_fish_aws_pool_size_total`: AWS instance pool size
- `aquarium_fish_aws_pool_usage_total`: AWS pool usage
- `aquarium_fish_aws_instance_cpu_percent`: AWS instance CPU usage
- `aquarium_fish_aws_instance_disk_usage_bytes`: AWS instance disk usage
- `aquarium_fish_aws_instance_network_bytes`: AWS instance network usage

### RPC Metrics

- `aquarium_fish_rpc_channel_load_total`: RPC channel load
- `aquarium_fish_rpc_connections_total`: Active RPC connections
- `aquarium_fish_rpc_streaming_clients_total`: Active streaming clients

## Tracing

Distributed tracing is automatically enabled for:

- **HTTP API Requests**: All incoming API requests
- **RPC Calls**: gRPC service calls and streaming operations
- **Database Operations**: Database reads, writes, and compaction
- **Driver Operations**: Resource provisioning and management
- **Test Execution**: Test spans for performance analysis

### Trace Attributes

Common trace attributes include:
- `service.name`: Service name
- `service.version`: Service version
- `node.name`: Fish node name
- `node.location`: Fish node location
- `operation.name`: Operation being performed
- `resource.type`: Type of resource involved

## Profiling

Continuous profiling captures:

- **CPU Profile**: CPU usage by function
- **Memory Profiles**: Heap allocations and usage
- **Goroutine Profile**: Goroutine stack traces
- **Mutex Profiles**: Lock contention analysis
- **Block Profiles**: Blocking operation analysis

Profiles are automatically tagged with:
- `node.name`: Fish node identifier
- `node.location`: Deployment location
- `version`: Service version

## Testing Integration

The monitoring system automatically instruments test execution:

```go
// Test spans are automatically created with:
// - Test name and package
// - Duration metrics
// - Pass/fail status
// - Performance profiling

func TestMyFeature(t *testing.T) {
    // Test execution is automatically traced
    fish := helper.NewAquariumFish(t, "test-node", testConfig)
    defer fish.Cleanup(t)

    // Test operations are automatically monitored
}
```

## Production Considerations

### Sampling Configuration

For production environments, reduce sampling rates:

```yaml
monitoring:
  sample_rate: 0.1  # 10% sampling for production
  metrics_interval: "30s"  # Less frequent metrics collection
```

### Resource Management

Monitor the monitoring overhead:
- Trace sampling reduces performance impact
- Metric collection interval affects precision vs. overhead
- Profiling data size grows with application complexity

### Security

- OTLP endpoints should be secured in production
- Consider network policies for monitoring traffic
- Profiling data may contain sensitive information

## Alerting

Configure alerts in Grafana for key metrics:

- High error rates: `rate(aquarium_fish_allocation_errors_total[5m]) > 0.1`
- High CPU usage: `aquarium_fish_cpu_usage_percent > 80`
- Certificate expiry: `aquarium_fish_cert_expiry_seconds < 604800` (7 days)
- Election failures: `rate(aquarium_fish_election_failures_total[5m]) > 0`

## Troubleshooting

### Common Issues

1. **No metrics appearing**:
   - Check OTLP endpoint connectivity
   - Verify monitoring is enabled in configuration
   - Check Fish logs for monitoring initialization errors

2. **High overhead**:
   - Reduce trace sampling rate
   - Increase metrics collection interval
   - Disable profiling if not needed

3. **Missing traces**:
   - Check trace sampling configuration
   - Verify Tempo is receiving data
   - Check for context propagation issues

### Debug Mode

Enable debug logging for monitoring:

```bash
./aquarium-fish --cfg config.yml --verbosity debug
```

Look for log entries prefixed with "Monitoring:" for diagnostic information.

## Examples

See the `examples/monitoring-config.yml` for a complete configuration example.

For dashboard examples and alerting rules, check the `docs/monitoring/` directory.
