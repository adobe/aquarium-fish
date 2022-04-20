#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Build fish for linux (using docker)

export root_dir=$(dirname "`realpath "$0"`")
export module=$(grep '^module' "${root_dir}/go.mod" | cut -d ' ' -f 2)

# Run in docker container
if [ "$(command -v go)" = "" -o "$(go env GOOS)" != "linux" ]; then
    docker run --rm -it -v "$root_dir":/go/src/${module}:z -w /go/src/${module} -e GOOS=linux -e GOARCH=amd64 golang:1.17 ./build-linux.sh
    exit 0
fi

export DEBIAN_FRONTEND=noninteractive

apt update
apt install -y autotools-dev autoconf patch libtool m4 automake

deps_dir=${root_dir}/deps
. deps/build-deps.sh

export SET_CGO_LDFLAGS="-static -pthread ${DQLITE_LIBS} ${SQLITE_LIBS} ${RAFT_LIBS} ${UV_LIBS} -lm -ldl"

cd "${root_dir}"
sh _build.sh
