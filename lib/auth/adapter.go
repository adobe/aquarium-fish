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

package auth

import (
	"github.com/casbin/casbin/v2/model"
)

// MemoryAdapter implements Casbin's persist.Adapter interface using in-memory storage
type MemoryAdapter struct {
	policies   [][]string
	roles      [][]string
	ownerships [][]string
}

// NewMemoryAdapter creates a new adapter instance
func NewMemoryAdapter() *MemoryAdapter {
	return &MemoryAdapter{
		policies:   make([][]string, 0),
		roles:      make([][]string, 0),
		ownerships: make([][]string, 0),
	}
}

// LoadPolicy loads policy rules from memory
func (a *MemoryAdapter) LoadPolicy(m model.Model) error {
	// Load policy rules
	for _, policy := range a.policies {
		if len(policy) == 3 {
			m.AddPolicy("p", "p", policy)
		}
	}

	// Load role assignments
	for _, role := range a.roles {
		if len(role) == 2 {
			m.AddPolicy("g", "g", role)
		}
	}

	// Load resource ownership
	for _, ownership := range a.ownerships {
		if len(ownership) == 2 {
			m.AddPolicy("g", "g2", ownership)
		}
	}

	return nil
}

// SavePolicy saves policy rules to memory
func (a *MemoryAdapter) SavePolicy(m model.Model) error {
	// Save policy rules
	a.policies = m.GetPolicy("p", "p")

	// Save role assignments
	a.roles = m.GetPolicy("g", "g")

	// Save resource ownership
	a.ownerships = m.GetPolicy("g", "g2")

	return nil
}

// AddPolicy adds a policy rule to memory
func (a *MemoryAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	switch {
	case sec == "p" && ptype == "p":
		a.policies = append(a.policies, rule)
	case sec == "g" && ptype == "g":
		a.roles = append(a.roles, rule)
	case sec == "g" && ptype == "g2":
		a.ownerships = append(a.ownerships, rule)
	}
	return nil
}

// RemovePolicy removes a policy rule from memory
func (a *MemoryAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	switch {
	case sec == "p" && ptype == "p":
		a.policies = removeRule(a.policies, rule)
	case sec == "g" && ptype == "g":
		a.roles = removeRule(a.roles, rule)
	case sec == "g" && ptype == "g2":
		a.ownerships = removeRule(a.ownerships, rule)
	}
	return nil
}

// RemoveFilteredPolicy removes policy rules that match the filter from memory
func (a *MemoryAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	switch {
	case sec == "p" && ptype == "p":
		a.policies = removeFilteredRule(a.policies, fieldIndex, fieldValues)
	case sec == "g" && ptype == "g":
		a.roles = removeFilteredRule(a.roles, fieldIndex, fieldValues)
	case sec == "g" && ptype == "g2":
		a.ownerships = removeFilteredRule(a.ownerships, fieldIndex, fieldValues)
	}
	return nil
}

// Helper functions to remove rules
func removeRule(rules [][]string, rule []string) [][]string {
	var result [][]string
	for _, r := range rules {
		if !stringSliceEqual(r, rule) {
			result = append(result, r)
		}
	}
	return result
}

func removeFilteredRule(rules [][]string, fieldIndex int, fieldValues []string) [][]string {
	var result [][]string
	for _, rule := range rules {
		matched := true
		for i, v := range fieldValues {
			if v != "" && rule[fieldIndex+i] != v {
				matched = false
				break
			}
		}
		if !matched {
			result = append(result, rule)
		}
	}
	return result
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
