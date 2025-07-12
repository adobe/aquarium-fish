#!/bin/sh -e
# Copyright 2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Author: Sergei Parshev (@sparshev)

set -e

# Get the directory of this script
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Building Web Dashboard ==="

# Check if we should only generate protobuf code
if [ "$1" = "gen-only" ]; then
    echo "Generating protobuf code only..."
    
    # Generate protobuf code using Docker
    docker run --rm \
        -v "${SCRIPT_DIR}:/workspace:rw" \
        -v "${SCRIPT_DIR}/../proto:/workspace/proto:ro" \
        -w /workspace \
        node:18-alpine \
        sh -c "
            # Install buf & generator plugin
            npm install -g @bufbuild/buf @bufbuild/protoc-gen-es
            
            # Generate protobuf code for web
            buf generate proto/
        "
    
    echo "Protobuf code generation completed"
    exit 0
fi

# Full build process
echo "Building web application..."

# Create necessary directories
mkdir -p dist

# Use Docker to build the web application
docker run --rm \
    -v "${SCRIPT_DIR}:/workspace:rw" \
    -v "${SCRIPT_DIR}/../proto:/workspace/proto:ro" \
    -w /workspace \
    node:18-alpine \
    sh -c "
        set -e
        
        echo 'Installing dependencies...'
        npm install
        
        echo 'Generating protobuf code...'
        # Install protobuf tools
        npm install -g @bufbuild/buf @bufbuild/protoc-gen-es
        
        # Generate protobuf code
        buf generate proto/
        
        echo 'Building application...'
        npm run build
        
        echo 'Build completed successfully'
    "

echo "=== Web Dashboard Build Completed ===" 
