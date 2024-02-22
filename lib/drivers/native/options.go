/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package native

import (
	"encoding/json"
	"fmt"
	os_user "os/user"
	"runtime"
	"text/template"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

/**
 * Options example:
 *   images:
 *     - url: https://artifact-storage/aquarium/image/native/macos-VERSION/macos-VERSION.tar.xz
 *       sum: sha256:1234567890abcdef1234567890abcdef1
 *       tag: ws  # The same as a name of disk in Label resource definition
 *     - url: https://artifact-storage/aquarium/image/native/macos_amd64-ci-VERSION/macos_amd64-ci-VERSION.tar.xz
 *       sum: sha256:1234567890abcdef1234567890abcdef2
 *       tag: ws
 *   entry: "{{ .Disks.ws }}/init.sh"  # CWD is user home
 *   groups:
 *     - staff
 *     - importantgroup
 */
type Options struct {
	Images []drivers.Image `json:"images"` // Optional list of image dependencies, they will be unpacked in order
	//TODO: Setup  string          `json:"setup"`  // Optional path to the executable, it will be started before the Entry with escalated priveleges
	Entry  string   `json:"entry"`  // Optional path to the executable, it will be running as workload (default: init.sh / init.ps1)
	Groups []string `json:"groups"` // Optional user groups user should have, first one is primary (default: staff)
}

func (o *Options) Apply(options util.UnparsedJson) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		return log.Error("Native: Unable to apply the driver definition", err)
	}

	return o.Validate()
}

// Note: there is no mandatory options, because in theory the native env could be pre-created
func (o *Options) Validate() error {
	// Set default entry
	if o.Entry == "" {
		if runtime.GOOS == "windows" {
			o.Entry = ".\\init.ps1"
		} else {
			// On other systems sh should work just fine
			o.Entry = "./init.sh"
		}
	}
	// Verify that entry template is ok
	if _, err := template.New("").Parse(o.Entry); err != nil {
		return fmt.Errorf("Native: Unable to parse entry template %q: %v", o.Entry, err)
	}

	// Set default user groups
	// The user is not complete without the primary group, so using current runtime user group
	if len(o.Groups) == 0 {
		u, e := os_user.Current()
		if e != nil {
			return log.Error("Native: Unable to get the current system user:", e)
		}
		group, e := os_user.LookupGroupId(u.Gid)
		if e != nil {
			return log.Error("Native: Unable to get the current system user group name:", u.Gid, e)
		}
		o.Groups = append(o.Groups, group.Name)
	}

	// Check images
	var img_err error
	for index, _ := range o.Images {
		if err := o.Images[index].Validate(); err != nil {
			img_err = log.Error("Native: Error during image validation:", err)
		}
	}
	if img_err != nil {
		return img_err
	}

	return nil
}
