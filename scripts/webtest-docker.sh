#!/bin/sh -e
# Copyright 2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Test allows to properly run webtests in docker with tons of playwright dependencies.
# Please set NOBUILD=1 if want to skip build of binary for linux
# Use first argument to specify the test or skip that and it will run all the tests

# Author: Sergei Parshev (@sparshev)

TEST="$@"
# If no args was specified - run all the tests
[ "x$TEST" != x ] || TEST='./webtests/...'

# Preparing playwright docker image if not exists
if [ "x$(docker images -q aquarium-fish-webtests-playwright)" = 'x' ]; then
    docker build --tag aquarium-fish-webtests-playwright -f- . <<EOF
FROM golang:1.23.1
WORKDIR /tmp/ws
COPY go.mod go.sum ./
RUN go run github.com/playwright-community/playwright-go/cmd/playwright install --with-deps && rm -f go.mod go.sum
EOF
fi

# Building for host os to generate & build web
[ "x$NOBUILD" != 'x' ] || SKIPCHECK=1 ./build.sh

docker run -v $PWD:/ws -v $HOME/go/pkg:/go/pkg -w /ws --rm -it aquarium-fish-webtests-playwright sh -exc "
[ 'x$NOBUILD' != 'x' ] || ONLYBUILD=1 NO_WEB=1 ./build.sh

echo '--- RUNNING WEBTESTS $TEST ---'
go test -json -v -parallel 4 -count=1 -race $TEST | \
    tee webtests_full.log | \
    go run ./tools/go-test-formatter/go-test-formatter.go -stdout_timestamp test -stdout_color -stdout_filter failed || \
    bash
"
