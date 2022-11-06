#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

#
# Simple test executing basic usecase of allocate/deallocate of the Application
#

cleanup() {
    echo "INFO: Cleanup..."
    cd /tmp

    # Kill the aquarium fish process & wait for it to exit
    if [ $aquarium_fish_pid ]; then
        echo "INFO:  stopping fish: $aquarium_fish_pid"
        kill $aquarium_fish_pid || true
        echo "INFO:  wait fish for exit"
        while kill -0 $aquarium_fish_pid > /dev/null 2>&1; do 
            sleep 1
        done
        echo "INFO:  stopping tail fish log process"
        pkill -f "tail -f fish.log" || true
    fi

    echo "INFO:  cleaning workspace dir $ws_dir"
    rm -rf -- "${ws_dir}"
    echo "INFO: Cleanup done"
}

curr_dir=${PWD}
ws_dir=$(mktemp -d -t fish_ci-XXXXXXXXXX)
cd "$ws_dir"

trap "cleanup" INT EXIT

# Prepare the test config
cat - <<EOF > "$ws_dir/config.yml"
node_name: node-1
node_location: test_loc

api_address: 127.0.0.1:8001

drivers:
  - name: test
EOF

# Run Aquarium node
"$curr_dir/aquarium-fish.linux_amd64" -c config.yml > fish.log 2>&1 &
aquarium_fish_pid=$!
tail -f fish.log &

# Getting the admin pass token
while true; do
    token=$(grep 'Admin user pass: ' fish.log | cut -d' ' -f 4)
    [ "x$token" = "x" ] || break
    sleep 1
done

echo "INFO: Got the admin token: $token"

# Wait for fish node init done
while ! grep -q 'Fish initialized' fish.log; do
    sleep 1
done

echo "INFO: Create the label"

label=test1
label_uid=$(curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/json' \
    -d '{"name":"'$label'", "version":1, "driver":"test", "definition": {}}' \
    https://127.0.0.1:8001/api/v1/label/ | grep -o '"UID": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')
echo "INFO: Created Label UID: ${label_uid}"

echo "INFO: Run application"

app_uid=$(curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/json' \
    -d '{"label_UID":"'$label_uid'"}' \
    https://127.0.0.1:8001/api/v1/application/ | grep -o '"UID": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')

echo "INFO: Created Application UID: ${app_uid}"

echo "INFO: Wait for status ALLOCATED"

for i in $(seq 10); do
    app_state=$(curl -s -u "admin:$token" -k \
        "https://127.0.0.1:8001/api/v1/application/$app_uid/state" | grep -o '"status": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')
    [ "$app_state" != "ALLOCATED" ] || break
    sleep 1
done

if [ "$app_state" != "ALLOCATED" ]; then
    echo "FAIL: Application did not reached ALLOCATED state in 10 seconds: $(curl -s -u "admin:$token" \
        -k "https://127.0.0.1:8001/api/v1/application/$app_uid/state")"
    exit 1
fi

echo "INFO: Checking if the Application Resource exists"

curl -s -u "admin:$token" -k "https://127.0.0.1:8001/api/v1/application/$app_uid/resource" | grep '"hw_addr":'

echo "INFO: Deallocating the Application"

curl -s -u "admin:$token" -k "https://127.0.0.1:8001/api/v1/application/$app_uid/deallocate"

echo "INFO: Wait for status DEALLOCATED"

for i in $(seq 10); do
    app_state=$(curl -s -u "admin:$token" -k \
        "https://127.0.0.1:8001/api/v1/application/$app_uid/state" | grep -o '"status": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')
    [ "$app_state" != "DEALLOCATED" ] || break
    sleep 1
done

if [ "$app_state" != "DEALLOCATED" ]; then
    echo "FAIL: Application did not reached DEALLOCATED state in 10 seconds: $(curl -s -u "admin:$token" \
        -k "https://127.0.0.1:8001/api/v1/application/$app_uid/state")"
    exit 1
fi

echo "INFO: Test is completed"
