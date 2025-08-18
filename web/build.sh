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

# Get the directory of this script
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

if [ "x$ONLYBUILD" = 'x' ]; then
    echo "=== Generating protobuf code ==="

    # Use Docker to build the web application
    docker run --rm \
        -v "${SCRIPT_DIR}:/workspace:rw" \
        -v "${SCRIPT_DIR}/../proto:/workspace/proto:ro" \
        -w /workspace \
        node:24-alpine \
        sh -ec "
            echo 'Installing dependencies...'
            npm --prefer-offline install

            echo 'Generating protobuf code...'
            npx @bufbuild/buf generate proto/
        "
fi

[ -z "$ONLYGEN" ] || exit 0

echo "=== Building Web Dashboard ==="

# Use Docker to build the web application
docker run --rm \
    -v "${SCRIPT_DIR}:/workspace:rw" \
    -v "${SCRIPT_DIR}/../proto:/workspace/proto:ro" \
    -w /workspace \
    node:24-alpine \
    sh -ec "
        echo 'Installing dependencies...'
        npm --prefer-offline install

        echo 'Running eslint...'
        # Disabled fail for now
        npx eslint ./app || true

        if [ "x$RELEASE" != 'x' ]; then
            echo 'Building release SPA...'
            npm run build
        else
            echo 'Building debug SPA...'
            #NODE_ENV=development DEBUG='*:*' npm run build:debug
            npm run build:debug
        fi
    "
