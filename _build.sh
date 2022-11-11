#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Common build script - do not run directly
# Use ./build-linux.sh / ./build-macos.sh instead

[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(go env GOOS)_$(go env GOARCH)"

echo "ROOT DIR: ${root_dir}"
cd "${root_dir}"


echo "--- GENERATE CODE FOR AQUARIUM-FISH ---"
# Install oapi-codegen if it's not available or version is not the same with go.mod
gopath=$(go env GOPATH)
req_ver=$(grep -F 'github.com/deepmap/oapi-codegen' go.mod | cut -d' ' -f 2)
curr_ver="$(PATH="$gopath/bin:$PATH" oapi-codegen --version 2>/dev/null | tail -1 || true)"
if [ "$curr_ver" != "$req_ver" ]; then
    go install "github.com/deepmap/oapi-codegen/cmd/oapi-codegen@$req_ver"
fi
# Cleanup the old generated files & run the generation
find ./lib -name '*.gen.go' -delete
PATH="$gopath/bin:$PATH" go generate -v ./lib/...

# Doing check after generation because generated sources requires additional modules
./check.sh

if [ "x${RELEASE}" != "x" ]; then
    export GIN_MODE=release
else
    echo
    echo "--- WARNING: build DEBUG mode ---"
fi


echo
echo "--- RUN UNIT TESTS ---"
go test -v ./lib/... ./cmd/...

echo
echo "--- BUILD AQUARIUM-FISH ---"
go build -ldflags="-s -w" -a -o "aquarium-fish.$suffix" ./cmd/fish

# Remove debug symbols
strip "aquarium-fish.$suffix"
