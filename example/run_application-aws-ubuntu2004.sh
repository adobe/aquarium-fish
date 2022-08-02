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
# This script creates the new Application to allocate resource of the latest version of Label
# Please check the Application metadata below - it defines the jenkins node to connect
#

token=$1
[ "$token" ] || exit 1

label=aws-ubuntu2004

# It's a bit dirty, but works for now - probably better to create API call to find the latest label
curr_label=$(curl -s -u "admin:$token" -k 'https://127.0.0.1:8001/api/v1/label/?filter=name="'$label'"' | sed 's/},{/},\n{/g' | tail -1)
curr_label_id="$(echo "$curr_label" | grep -o '"ID": *[0-9]\+,' | tr -dc '0-9')"
if [ "x$curr_label_id" = "x" ]; then
    echo "ERROR: Unable to find label '$label' - please create one before running the application"
    exit 1
fi

echo "Found label '$label': $curr_label"

echo
echo "Press key to create the Application with label '$label'"
read w1

app_id=$(curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/json' -d '{"label_ID":'$curr_label_id', "metadata":{
    "JENKINS_URL": "https://jenkins-host.local/",
    "JENKINS_AGENT_SECRET": "03839eabcf945b1e780be8f9488d264c4c57bf388546da9a84588345555f29b0",
    "JENKINS_AGENT_NAME": "test-node"
}}' https://127.0.0.1:8001/api/v1/application/ | grep -o '"ID": *[0-9]\+,' | tr -dc '0-9')
echo "Application ID: ${app_id}"

echo "Press key to check the application resource"
read w1

curl -u "admin:$token" -k https://127.0.0.1:8001/api/v1/application/$app_id/resource

echo "Press key to deallocate the application resource"
read w1

curl -u "admin:$token" -k https://127.0.0.1:8001/api/v1/application/$app_id/deallocate
