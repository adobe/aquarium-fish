#!/bin/sh -e
# Copyright 2021-2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Please set NOBUILD=1 if want to skip build of binary for linux
# Use first argument to specify the test or skip that and it will run all the tests

# Author: Sergei Parshev (@sparshev)

TEST="$@"

if [ "$(echo "x$TEST" | cut -c-2)" = 'x-' ]; then
    test_cmd="$TEST"
elif [ "x$TEST" != 'x' ]; then
    test_cmd="-run '^$TEST\$'"
else
    test_cmd="-skip '_stress\$'"
fi

docker run -v $PWD:/ws -w /ws --rm -it golang:1.23.1 sh -exc "
[ 'x$NOBUILD' != 'x' ] || SKIPCHECK=1 ./build.sh

counter=0
fish_bin=\$(ls -t aquarium-fish-*.linux_amd64 | head -1)

while true
do
    FISH_PATH=\$PWD/\$fish_bin go test -v -failfast -parallel 1 -count=1 $test_cmd ./tests

    counter=\$((\$counter+1))
    if [ \$counter -ge 50 ]; then
        break
    fi
    sleep 1
done

echo \$counter
"
