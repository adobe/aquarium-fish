#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.


token=$1
[ "$token" ] || exit 1

label=vs2019
version=3

echo "Get or create the label"

label_id=$(curl -u "admin:$token" -k 'https://127.0.0.1:8001/api/v1/label/?filter=name="'$label'"%20AND%20version="'$version'"' | grep -o '"ID": *[0-9]\+,' | tr -dc '0-9')

if [ -z "$label_id" ]; then
    label_id=$(curl -u "admin:$token" -k -X POST -H 'Content-Type: application/json' -d '{"name":"'$label'", "version":'$version', "driver":"vmx",
        "definition": {
            "image": "winserver2019-ci-vs2019",
            "images": {
                "winserver2019": "https://artifact-storage/aquarium/image/winserver2019-VERSION/winserver2019-VERSION.tar.xz",
                "winserver2019-ci": "https://artifact-storage/aquarium/image/winserver2019-ci-VERSION/winserver2019-ci-VERSION.tar.xz",
                "winserver2019-ci-vs2019": "https://artifact-storage/aquarium/image/winserver2019-ci-vs2019-VERSION/winserver2019-ci-vs2019-VERSION.tar.xz"
            },
            "requirements": {
                "cpu": 14,
                "ram": 12,
                "disks": {
                    "vs2019": {
                        "type": "exfat",
                        "size": 100,
                        "reuse": true
                    }
                }
            }
        },
        "metadata": {
            "JENKINS_AGENT_WORKSPACE": "D:\\"
        }
    }' https://127.0.0.1:8001/api/v1/label/ | grep -o '"ID": *[0-9]\+,' | tr -dc '0-9')
fi
echo "Label ID: ${label_id}"

echo "Press key to get label"
read w1

curl -u "admin:$token" -k https://127.0.0.1:8001/api/v1/label/${label_id}

echo "Press key to run application"
read w1

app_id=$(curl -u "admin:$token" -k -X POST -H 'Content-Type: application/json' -d '{"label_ID":'$label_id', "metadata":{
    "JENKINS_URL": "http://172.16.1.1:8085/",
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
