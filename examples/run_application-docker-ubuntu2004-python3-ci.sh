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

#
# This script creates the new Application to allocate resource of the latest version of Label
# Please check the Application metadata below - it defines the jenkins node to connect
#

token=$1
[ "$token" ] || exit 1
hostport=$2
[ "$hostport" ] || hostport=localhost:8001

label=ubuntu2004-python3_docker

curr_label=$(curl -s -u "admin:$token" -X POST --header "Content-Type: application/json" \
    -d "{\"name\":\"$label\",\"version\":\"last\"}" -k "https://$hostport/grpc/aquarium.v2.LabelService/List" | sed 's/^.*"data":\[//' | sed 's/\]}$//')
curr_label_id="$(echo "$curr_label" | grep -o '"uid": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')"
if [ "x$curr_label_id" = "x" ]; then
    echo "ERROR: Unable to find label '$label' - please create one before running the application"
    exit 1
fi

echo "Found label '$label': $curr_label_id : $curr_label"

echo
echo "Press key to create the Application with label '$label'"
read w1

app=$(curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/yaml' -d '---
application:
  label_uid: '$curr_label_id'
  metadata:
    JENKINS_URL: http://host.docker.internal:8085/
    JENKINS_AGENT_SECRET: 03839eabcf945b1e780be8f9488d264c4c57bf388546da9a84588345555f29b0
    JENKINS_AGENT_NAME: test-node
' "https://$hostport/grpc/aquarium.v2.ApplicationService/Create")
app_id="$(echo "$app" | grep -o '"UID": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')"

echo "Application created: $app_id : $app"

echo "Press key to check the application resource"
read w1

curl -u "admin:$token" -X POST -d "{\"application_uid\":\"$app_id\"}" -k "https://$hostport/grpc/aquarium.v2.ApplicationService/GetResource"

echo "Press key to deallocate the application resource"
read w1

curl -u "admin:$token" -X POST -d "{\"application_uid\":\"$app_id\"}" -k "https://$hostport/grpc/aquarium.v2.ApplicationService/Deallocate"
