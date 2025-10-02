/**
 * Copyright 2025 Adobe. All rights reserved.
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

package database

import (
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// SubscriptionEvent represents a database change event with change type
type SubscriptionEvent[T any] struct {
	ChangeType aquariumv2.ChangeType
	Object     T
}

// ApplicationSubscriptionEvent represents an Application change event
type ApplicationSubscriptionEvent = SubscriptionEvent[*typesv2.Application]

// ApplicationStateSubscriptionEvent represents an ApplicationState change event
type ApplicationStateSubscriptionEvent = SubscriptionEvent[*typesv2.ApplicationState]

// ApplicationTaskSubscriptionEvent represents an ApplicationTask change event
type ApplicationTaskSubscriptionEvent = SubscriptionEvent[*typesv2.ApplicationTask]

// ApplicationResourceSubscriptionEvent represents an ApplicationResource change event
type ApplicationResourceSubscriptionEvent = SubscriptionEvent[*typesv2.ApplicationResource]

// LabelSubscriptionEvent represents a Label change event
type LabelSubscriptionEvent = SubscriptionEvent[*typesv2.Label]

// UserSubscriptionEvent represents a User change event
type UserSubscriptionEvent = SubscriptionEvent[*typesv2.User]

// RoleSubscriptionEvent represents a Role change event
type RoleSubscriptionEvent = SubscriptionEvent[*typesv2.Role]

// UserGroupSubscriptionEvent represents a UserGroup change event
type UserGroupSubscriptionEvent = SubscriptionEvent[*typesv2.UserGroup]

// NodeSubscriptionEvent represents a Node change event
type NodeSubscriptionEvent = SubscriptionEvent[*typesv2.Node]

// Helper functions to create events
func NewCreateEvent[T any](obj T) SubscriptionEvent[T] {
	return SubscriptionEvent[T]{
		ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_CREATED,
		Object:     obj,
	}
}

func NewUpdateEvent[T any](obj T) SubscriptionEvent[T] {
	return SubscriptionEvent[T]{
		ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_UPDATED,
		Object:     obj,
	}
}

func NewRemoveEvent[T any](obj T) SubscriptionEvent[T] {
	return SubscriptionEvent[T]{
		ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_REMOVED,
		Object:     obj,
	}
}
