/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package native

import (
	"encoding/json"
	"fmt"
	osuser "os/user"
	"runtime"
	"text/template"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Options for label definition
//
// Example:
//
//	entry: "{{ .Disks.ws }}/init.sh"  # CWD is user home
//	groups:
//	  - staff
//	  - importantgroup
type Options struct {
	// TODO: Setup  string          `json:"setup"`  // Optional path to the executable, it will be started before the Entry with escalated privileges
	Entry  string   `json:"entry"`  // Optional path to the executable, it will be running as workload (default: init.sh / init.ps1)
	Groups []string `json:"groups"` // Optional user groups user should have, first one is primary (default: staff)
}

// Apply takes json and applies it to the options structure
func (o *Options) Apply(options util.UnparsedJSON) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		log.WithFunc("native", "Apply").Error("Unable to apply the driver definition", "err", err)
		return fmt.Errorf("Native: Unable to apply the driver definition: %v", err)
	}

	return o.Validate()
}

// Validate makes sure the options have the required defaults & that the required fields are set
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
		u, e := osuser.Current()
		if e != nil {
			log.WithFunc("native", "Validate").Error("Unable to get the current system user", "err", e)
			return fmt.Errorf("Native: Unable to get the current system user: %v", e)
		}
		group, e := osuser.LookupGroupId(u.Gid)
		if e != nil {
			log.WithFunc("native", "Validate").Error("Unable to get the current system user group name", "user_gid", u.Gid, "err", e)
			return fmt.Errorf("Native: Unable to get the current system user group name %s: %v", u.Gid, e)
		}
		o.Groups = append(o.Groups, group.Name)
	}

	return nil
}
