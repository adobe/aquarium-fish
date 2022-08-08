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


gopath=$(go env GOPATH)

echo "--- PATCH OAPI-CODEGEN ---"
# Generate the API code patch
go get -d "github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.11.1-0.20220908201945-d1a63c702fd0"
chmod -R u+w "$gopath/pkg/mod/github.com/deepmap/oapi-codegen@v1.11.1-0.20220908201945-d1a63c702fd0"
patch -N -p1 -d "$gopath/pkg/mod/github.com/deepmap/oapi-codegen@v1.11.1-0.20220908201945-d1a63c702fd0" < "${root_dir}/deps/oapi-codegen.patch" || true

echo "--- GENERATE CODE FOR AQUARIUM-FISH ---"
find ./lib -name '*.gen.go' -delete
go generate -v ./lib/...

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
