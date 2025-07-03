#!/bin/bash
# Copyright 2021-2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Author: Sergei Parshev (@sparshev)

# start-monitoring.sh - Quick setup script for Aquarium Fish monitoring stack
# This script starts the Grafana OTEL LGTM container for local development

set -e

CONTAINER_NAME="aquarium-fish-monitoring"
GRAFANA_PORT="3000"
OTLP_GRPC_PORT="4317"
OTLP_HTTP_PORT="4318"
PROMETHEUS_PORT="9090"
PYROSCOPE_PORT="4040"

echo "🐟 Starting Aquarium Fish Monitoring Stack..."

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo "❌ Error: Docker is not running. Please start Docker and try again."
    exit 1
fi

# Stop existing container if running
if docker ps -q -f name="$CONTAINER_NAME" | grep -q .; then
    echo "🛑 Stopping existing monitoring container..."
    docker stop "$CONTAINER_NAME" >/dev/null
fi

# Remove existing container if exists
if docker ps -aq -f name="$CONTAINER_NAME" | grep -q .; then
    echo "🗑️  Removing existing monitoring container..."
    docker rm "$CONTAINER_NAME" >/dev/null
fi

# Check if ports are available
check_port() {
    local port=$1
    local service=$2
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo "⚠️  Warning: Port $port is already in use. $service may not work correctly."
        return 1
    fi
    return 0
}

echo "🔍 Checking port availability..."
check_port $GRAFANA_PORT "Grafana"
check_port $OTLP_GRPC_PORT "OTLP gRPC"
check_port $OTLP_HTTP_PORT "OTLP HTTP"
check_port $PROMETHEUS_PORT "Prometheus"
check_port $PYROSCOPE_PORT "Pyroscope"

# Start the monitoring stack
echo "🚀 Starting Grafana OTEL LGTM container..."
docker run -d \
    --name "$CONTAINER_NAME" \
    -p $GRAFANA_PORT:3000 \
    -p $OTLP_GRPC_PORT:4317 \
    -p $OTLP_HTTP_PORT:4318 \
    -p $PROMETHEUS_PORT:9090 \
    -p $PYROSCOPE_PORT:4040 \
    --restart unless-stopped \
    grafana/otel-lgtm:latest

# Wait for container to start
echo "⏳ Waiting for services to start..."
sleep 10

# Check if container is running
if ! docker ps -q -f name="$CONTAINER_NAME" | grep -q .; then
    echo "❌ Error: Failed to start monitoring container"
    docker logs "$CONTAINER_NAME"
    exit 1
fi

# Wait for Grafana to be ready
echo "🔄 Waiting for Grafana to be ready..."
timeout=60
while ! curl -s "http://localhost:$GRAFANA_PORT/api/health" >/dev/null 2>&1; do
    if [ $timeout -le 0 ]; then
        echo "❌ Error: Grafana failed to start within 60 seconds"
        docker logs "$CONTAINER_NAME"
        exit 1
    fi
    sleep 2
    timeout=$((timeout - 2))
    echo -n "."
done
echo ""

# Success message
cat << EOF

✅ Monitoring stack started successfully!

📊 Access your monitoring services:
   • Grafana:    http://localhost:$GRAFANA_PORT (admin/admin)
   • Prometheus: http://localhost:$PROMETHEUS_PORT
   • Pyroscope:  http://localhost:$PYROSCOPE_PORT

🔌 OTLP Endpoints:
   • gRPC: localhost:$OTLP_GRPC_PORT
   • HTTP: localhost:$OTLP_HTTP_PORT

🐟 To start Fish with monitoring, use:
   ./aquarium-fish --cfg examples/monitoring-config.yml

📖 Documentation:
   See docs/monitoring.md for complete setup guide

🛑 To stop the monitoring stack:
   docker stop $CONTAINER_NAME

EOF

# Optional: Show container logs
read -p "📝 Show container logs? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "📄 Container logs:"
    docker logs "$CONTAINER_NAME"
fi

echo "🎉 Setup complete! Happy monitoring!"
