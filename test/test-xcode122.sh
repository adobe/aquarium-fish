#!/bin/sh -e

token=$1

label=xcode12.2

echo "Get or create the label"

label_id=$(curl -u "admin:$token" '127.0.0.1:8001/api/v1/label/?filter=name="'$label'"' | grep -o '"ID":[0-9]\+,' | tr -dc '0-9')

if [ -z "$label_id" ]; then
    label_id=$(curl -u "admin:$token" -X POST -d '{"name":"'$label'", "version":1, "driver":"vmx",
        "definition": {
            "image": "macos-1015-ci-xcode122",
            "images": {
                "macos-1015": "https://artifactory.corp.adobe.com/artifactory/maven-ci-tools-snapshot/com/adobe/ci/image/macos-1015-VERSION/macos-1015-VERSION.tar.xz",
                "macos-1015-ci": "https://artifactory.corp.adobe.com/artifactory/maven-ci-tools-snapshot/com/adobe/ci/image/macos-1015-ci-VERSION/macos-1015-ci-VERSION.tar.xz",
                "macos-1015-ci-xcode122": "https://artifactory.corp.adobe.com/artifactory/maven-ci-tools-snapshot/com/adobe/ci/image/macos-1015-ci-xcode122-VERSION/macos-1015-ci-xcode122-VERSION.tar.xz"
            },
            "requirements": {
                "cpu": 14,
                "ram": 12,
                "disks": {
                    "xcode122": {
                        "size": 100,
                        "reuse": true
                    }
                }
            }
        },
        "metadata": {
            "JENKINS_AGENT_WORKSPACE": "/Volumes/xcode122"
        }
    }' 127.0.0.1:8001/api/v1/label/ | grep -o '"ID":[0-9]\+,' | tr -dc '0-9')
fi

echo "Press key to get label"
read w1

curl -u "admin:$token" 127.0.0.1:8001/api/v1/label/${label_id}

echo "Press key to run application"
read w1

app_id=$(curl -u "admin:$token" -X POST -d '{"label_id":'$label_id', "metadata":{
    "JENKINS_URL": "http://172.16.1.1:8085/",
    "JENKINS_AGENT_SECRET": "03839eabcf945b1e780be8f9488d264c4c57bf388546da9a84588345555f29b0",
    "JENKINS_AGENT_NAME": "test-node"
}}' 127.0.0.1:8001/api/v1/application/ | grep -o '"ID":[0-9]\+,' | tr -dc '0-9')

echo "Press key to check the application resource"
read w1

curl -u "admin:$token" 127.0.0.1:8001/api/v1/application/$app_id/resource

echo "Press key to deallocate the application resource"
read w1

curl -u "admin:$token" 127.0.0.1:8001/api/v1/application/$app_id/deallocate
