#!/bin/sh -e
# Copyright 2021-2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Author: Sergei Parshev (@sparshev)

# Script is a part of build.sh to prepare for the build, it includes generation and checks...

root_dir=$(
    cd "$(dirname "$0")"
    echo "$PWD"
)
echo "ROOT DIR: ${root_dir}"
cd "${root_dir}"

echo "--- GENERATE CODE FOR AQUARIUM-FISH ---"

# Install required tools if not available
gopath=$(go env GOPATH)
export PATH="$PATH:$gopath/bin"

# Install buf for protobuf management if not available
if ! command -v buf >/dev/null 2>&1; then
    go install github.com/bufbuild/buf/cmd/buf@v1.54.0
fi

if ! command -v protoc-gen-go >/dev/null 2>&1; then
    # Version is from go.mod
    go install google.golang.org/protobuf/cmd/protoc-gen-go
fi

if ! command -v protoc-gen-connect-go >/dev/null 2>&1; then
    go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.18.1
fi

# Run code generation
go generate -v .

echo
echo "--- GENERATE PROTOBUF WEB ---"
# Build the web dashboard
if [ "x${NO_WEB}" = x ]; then
    cd web
    ONLYGEN=1 ./build.sh
    cd ..
else
    echo "Skipping. Reusing existing web dashboard build"
fi

# If ONLYGEN is specified - skip the build
[ -z "$ONLYGEN" ] || exit 0

# Doing check after generation because generated sources requires additional modules
[ "$SKIPCHECK" ] || ./check.sh

echo
echo "--- RUN UNIT TESTS ---"
# Unit tests should not consume more then 5 sec per run - for that we have integration tests
go test -v -failfast -count=1 -timeout=5s -v ./lib/...
