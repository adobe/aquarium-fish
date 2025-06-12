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

// Package implements interface for each ApplicationResource provider
package provider

import (
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Status of the driver returned by Status()
const (
	StatusNone      = "NONE"
	StatusAllocated = "ALLOCATED"
)

// FactoryList is a list of available drivers factories
var FactoryList []DriverFactory

// DriverFactory allows to generate new instances of the drivers
type DriverFactory interface {
	// Name of the driver
	Name() string

	// Generates new provider driver
	New() Driver
}

// Driver interface of the functions that connects Fish to each driver
type Driver interface {
	// Name of the driver
	Name() string

	// SetName of the gate
	SetName(name string)

	// If the driver uses local node resources or a cloud or remote resources
	// it is used to calculate the slots available for the local drivers
	IsRemote() bool

	// Give driver configs and check if it's ok
	// -> config - driver configuration in json format
	Prepare(config []byte) error

	// Make sure the allocate definition is appropriate for the driver
	// -> def - describes the driver options to allocate the required resource
	ValidateDefinition(def types.LabelDefinition) error

	// Check if the described definition can be running on the current node
	// -> node_usage - how much of node resources was used by all the drivers. Usually should not be used by the cloud drivers
	// -> req - definition describes requirements for the resource
	// <- capacity - the number of such definitions the driver could run, if -1 - error happened
	AvailableCapacity(nodeUsage types.Resources, req types.LabelDefinition) (capacity int64)

	// Allocate the resource by definition and returns hw address
	// -> def - describes the driver options to allocate the required resource
	// -> metadata - user metadata to use during resource allocation
	// <- res - initial resource information to store driver instance state
	Allocate(def types.LabelDefinition, metadata map[string]any) (res *types.ApplicationResource, err error)

	// Get the status of the resource with given hw address
	// -> res - resource information with stored driver instance state
	// <- status - current status of the resource
	Status(res *types.ApplicationResource) (status string, err error)

	// Get task struct with implementation to execute it later
	// -> task - identifier of the task operation
	// -> options - additional config options for the task
	GetTask(task, options string) DriverTask

	// Deallocate resource with provided hw addr
	// -> res - resource information with stored driver instance state
	Deallocate(res *types.ApplicationResource) error
}
