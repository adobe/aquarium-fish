#/bin/sh
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Script to simplify the style check process

root_dir=$(realpath "$(dirname "$0")")
errors=0

echo
echo '---------------------- Custom Checks ----------------------'
echo
for f in `git ls-files`; do
    # Check text files
    if file "$f" | grep -q 'text$'; then
        # Ends with newline as POSIX requires
        if [ -n "$(tail -c 1 "$f")" ]; then
            echo "ERROR: Should end with newline: $f"
            errors=$((${errors}+1))
        fi

        # Logic files: go, proto, sh
        if echo "$f" | grep -q '\.\(go\|proto\|sh\)$'; then
            # Should contain copyright
            if !(head -20 "$f" | grep -q 'Copyright 20.. Adobe. All rights reserved'); then
                echo "ERROR: Should contain Adobe copyright header: $f"
                errors=$((${errors}+1))
            fi

            # Should contain license
            if !(head -20 "$f" | grep -q 'Apache License, Version 2.0'); then
                echo "ERROR: Should contain license name and version: $f"
                errors=$((${errors}+1))
            fi

            #  Should contain Author
            #if !(head -20 "$f" | grep -q 'Author: .\+'); then
            #    echo "ERROR: Should contain Author: $f"
            #    errors=$((${errors}+1))
            #fi
        fi
    fi
done


echo
echo '---------------------- GoFmt verify ----------------------'
echo
reformat=$(gofmt -l -s . 2>&1)
if [ "${reformat}" ]; then
    echo "ERROR: Please run 'gofmt -s -w .': \n${reformat}"
    errors=$((${errors}+$(echo "${reformat}" | wc -l)))
fi


echo
echo '---------------------- GoModTidy verify ----------------------'
echo
cp -af go.mod go.sum /tmp/
tidy=$(go mod tidy -v)
if [ "${tidy}" -o "x$(date -r /tmp/go.mod ; date -r /tmp/go.sum)" != "x$(date -r go.mod ; date -r go.sum)" ]; then
    echo "ERROR: Please run 'go mod tidy -v' \n${tidy}"
    errors=$((${errors}+$(echo "${tidy}" | wc -l)))
fi
mv /tmp/go.mod /tmp/go.sum ./


echo
echo '---------------------- GoVet verify ----------------------'
echo
vet=$(go vet ./... 2>&1)
if [ "${vet}" ]; then
    echo "ERROR: Please fix the issues: \n${vet}"
    errors=$(( ${errors}+$(echo "${vet}" | wc -l) ))
fi


echo
echo '---------------------- Proto verify ----------------------'
echo
buf=$(cd proto; buf lint --config ../buf.yaml 2>&1)
if [ "${buf}" ]; then
    echo "ERROR: Please fix the issues: \n${buf}"
    errors=$(( ${errors}+$(echo "${buf}" | wc -l) ))
fi


echo
echo '---------------------- YAML Lint ----------------------'
echo
if command -v docker >/dev/null; then
    docker run --rm -v "${root_dir}:/data" cytopia/yamllint:1.22 --strict docs lib
    errors=$((${errors}+$?))
else
    # TODO: Find some useful yaml lint in go ecosystem
    echo 'WARN: Skipping, no docker installed'
fi

exit ${errors}
