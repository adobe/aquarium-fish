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

	// If the driver uses local node resources or a cloud or remote resources
	// it is used to calculate the slots available for the local drivers
	IsRemote() bool

	// Give driver configs and check if it's ok
	// -> config - driver configuration in json format
	// -> nodedef - information about the node the driver is running on
	Prepare(config []byte) error

	// Make sure the allocate definition is appropriate for the driver
	// -> definition - describes the driver options to allocate the required resource
	ValidateDefinition(definition string) error

	// Returns the defined Resources structure filled from definition
	// -> definition - describes the driver options to allocate the required resource
	DefinitionResources(definition string) Resources

	// Check if the described definition can be running on the current node
	// -> node_usage - how much of node resources was used by all the drivers. Usually should not be used by the cloud drivers
	// -> definition - describes the driver options to allocate the required resource
	// <- capacity - the number of such definitions the driver could run, if -1 - error happened
	AvailableCapacity(node_usage Resources, definition string) (capacity int64)

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
