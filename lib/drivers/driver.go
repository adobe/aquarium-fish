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

package drivers

const (
	StatusNone      = "NONE"
	StatusAllocated = "ALLOCATED"
)

var DriversList []ResourceDriver

type ResourceDriver interface {
	// Name of the driver
	Name() string

	// Give driver configs and check if it's ok
	// -> config - driver configuration in json format
	Prepare(config []byte) error

	// Make sure the allocate definition is appropriate
	// -> definition - describes the driver options to allocate the required resource
	ValidateDefinition(definition string) error

	// Allocate the resource by definition and returns hw address
	// -> definition - describes the driver options to allocate the required resource
	// -> metadata - user metadata to use during resource allocation
	// <- hwaddr - mandatory, needed to identify the resource. If it's a MAC address - it is used to auth in Meta API
	// <- ipaddr - optional, if driver can provide the assigned IP address of the instance
	Allocate(definition string, metadata map[string]interface{}) (hwaddr, ipaddr string, err error)

	// Get the status of the resource with given hw address
	Status(hwaddr string) string

	// Makes environment snapshot of the resource with given hw address
	// -> hwaddr - driver identifier of the resource
	// -> full - will try it's best to make the complete snapshot of the environment, else just non-image data (attached disks)
	// <- info - where to find the snapshots
	Snapshot(hwaddr string, full bool) (info string, err error)

	// Deallocate resource with provided hw addr
	// -> hwaddr - driver identifier of the resource
	Deallocate(hwaddr string) error
}
