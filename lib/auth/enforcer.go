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

package auth

import (
	"context"
	"embed"
	"fmt"
	"sync"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
)

//go:embed model.conf
var modelFS embed.FS

// Enforcer wraps Casbin enforcer with additional functionality
type Enforcer struct {
	enforcer *casbin.Enforcer
	adapter  persist.Adapter
	mu       sync.RWMutex

	running       context.Context //nolint:containedctx // Is used for sending stop for goroutines
	runningCancel context.CancelFunc

	roleUpdateChannel chan database.RoleSubscriptionEvent
}

var (
	// Global enforcer instance
	globalEnforcer *Enforcer
	// Mutex to protect enforcer initialization
	enforcerMutex sync.Mutex
)

// GetEnforcer returns the global enforcer instance
func GetEnforcer() *Enforcer {
	return globalEnforcer
}

// SetEnforcer sets the global enforcer instance
func SetEnforcer(e *Enforcer) {
	enforcerMutex.Lock()
	defer enforcerMutex.Unlock()
	globalEnforcer = e
}

// NewEnforcer creates a new Casbin enforcer with the embedded model and memory adapter
func NewEnforcer() (*Enforcer, error) {
	// Load model from embedded file
	modelText, err := modelFS.ReadFile("model.conf")
	if err != nil {
		return nil, fmt.Errorf("failed to read model file: %w", err)
	}

	m, err := model.NewModelFromString(string(modelText))
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	// Create memory adapter
	adapter := NewMemoryAdapter()

	// Create enforcer
	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}

	// Load policies from storage
	if err := enforcer.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load policy: %w", err)
	}

	e := &Enforcer{
		enforcer: enforcer,
		adapter:  adapter,
		mu:       sync.RWMutex{},
	}

	e.running, e.runningCancel = context.WithCancel(context.Background())

	// Set as global enforcer if not already set
	if globalEnforcer == nil {
		SetEnforcer(e)
	}

	return e, nil
}

func (e *Enforcer) SetUpdateChannel(ch chan database.RoleSubscriptionEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	previouslyStarted := e.roleUpdateChannel != nil
	e.roleUpdateChannel = ch

	// Running background process
	if !previouslyStarted && e.roleUpdateChannel != nil {
		log.WithFunc("auth", "SetUpdateChannel").Debug("Enforcer started background update process")
		go e.roleUpdatedProcess()
	}
}

// roleUpdatedProcess is listening on the update channel and makes modifications to enforcer permissions
func (e *Enforcer) roleUpdatedProcess() {
	logger := log.WithFunc("auth", "roleUpdatedProcess")
	for {
		select {
		case <-e.running.Done():
			return
		case roleEvent := <-e.roleUpdateChannel:
			r := roleEvent.Object
			switch roleEvent.ChangeType {
			case aquariumv2.ChangeType_CHANGE_TYPE_CREATED, aquariumv2.ChangeType_CHANGE_TYPE_UPDATED:
				for _, p := range r.Permissions {
					if err := e.AddPolicy(r.Name, p.Resource, p.Action); err != nil {
						logger.Error("Failed to set role permission", "role", r.Name, "permission", p, "err", err)
					}
				}
			case aquariumv2.ChangeType_CHANGE_TYPE_REMOVED:
				for _, p := range r.Permissions {
					if err := e.RemovePolicy(r.Name, p.Resource, p.Action); err != nil {
						logger.Error("Failed to remove role permission", "role", r.Name, "permission", p, "err", err)
					}
				}
			}
		}
	}
}

// CheckPermission checks if the roles has permission to perform the action on the object
func (e *Enforcer) CheckPermission(roles []string, obj, act string) bool {
	logger := log.WithFunc("auth", "CheckPermission").With("roles", roles, "obj", obj, "act", act)
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, role := range roles {
		allowed, err := e.enforcer.Enforce(role, obj, act)
		if err != nil {
			logger.Error("Enforcer: BLOCKED: Failed to check permission", "err", err)
			return false
		}
		if allowed {
			logger.Debug("Enforcer: PASSED")
			return true
		}
	}
	logger.Debug("Enforcer: BLOCKED")
	return false
}

// AddPolicy adds a new policy rule
func (e *Enforcer) AddPolicy(sub, obj, act string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, err := e.enforcer.AddPolicy(sub, obj, act)
	if err != nil {
		return fmt.Errorf("failed to add policy: %w", err)
	}
	return nil
}

// RemovePolicy removes a policy rule
func (e *Enforcer) RemovePolicy(sub, obj, act string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, err := e.enforcer.RemovePolicy(sub, obj, act)
	if err != nil {
		return fmt.Errorf("failed to remove policy: %w", err)
	}
	return nil
}

// AddRoleForUser adds a role for a user
func (e *Enforcer) AddRoleForUser(user, role string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, err := e.enforcer.AddGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("failed to add role for user: %w", err)
	}
	return nil
}

// AddResourceForUser adds a resource ownership for a user
func (e *Enforcer) AddResourceForUser(user, resource string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, err := e.enforcer.AddNamedGroupingPolicy("g2", user, resource)
	if err != nil {
		return fmt.Errorf("failed to add resource for user: %w", err)
	}
	return nil
}

// GetRolesForUser gets roles for a user
func (e *Enforcer) GetRolesForUser(user string) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.enforcer.GetRolesForUser(user)
}

// GetUsersForRole gets users that have a role
func (e *Enforcer) GetUsersForRole(role string) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.enforcer.GetUsersForRole(role)
}

// GetResourcesForUser gets resources owned by a user
func (e *Enforcer) GetResourcesForUser(user string) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	policies := e.enforcer.GetNamedGroupingPolicy("g2")
	var resources []string
	for _, policy := range policies {
		if len(policy) >= 2 && policy[0] == user {
			resources = append(resources, policy[1])
		}
	}
	return resources, nil
}

// Shutdown stops enforcer background processes
func (e *Enforcer) Shutdown() {
	e.runningCancel()
}
