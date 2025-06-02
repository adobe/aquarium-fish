package auth

import (
	"fmt"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/casbin/casbin/v2/model"
)

const (
	dbCasbinPrefix = "casbin"
	dbPolicyKey    = "policy"
	dbRoleKey      = "role"
	dbOwnerPrefix  = "owner"
)

// DatabaseAdapter implements Casbin's persist.Adapter interface
type DatabaseAdapter struct {
	db    *database.Database
	model model.Model // Store model reference
}

// NewDatabaseAdapter creates a new adapter instance
func NewDatabaseAdapter(db *database.Database) *DatabaseAdapter {
	return &DatabaseAdapter{db: db}
}

// LoadPolicy loads policy rules from database
func (a *DatabaseAdapter) LoadPolicy(m model.Model) error {
	// Store the model reference
	a.model = m

	// Load policy rules
	var policies [][]string
	if err := a.db.Get(dbCasbinPrefix, dbPolicyKey, &policies); err != nil && err != database.ErrObjectNotFound {
		return fmt.Errorf("failed to load policy: %w", err)
	}
	if policies != nil {
		for _, policy := range policies {
			if len(policy) == 3 {
				m.AddPolicy("p", "p", policy)
			}
		}
	}

	// Load role assignments
	var roles [][]string
	if err := a.db.Get(dbCasbinPrefix, dbRoleKey, &roles); err != nil && err != database.ErrObjectNotFound {
		return fmt.Errorf("failed to load roles: %w", err)
	}
	if roles != nil {
		for _, role := range roles {
			if len(role) == 2 {
				m.AddPolicy("g", "g", role)
			}
		}
	}

	// Load resource ownership
	var ownerships [][]string
	if err := a.db.Get(dbCasbinPrefix, dbOwnerPrefix, &ownerships); err != nil && err != database.ErrObjectNotFound {
		return fmt.Errorf("failed to load ownership: %w", err)
	}
	if ownerships != nil {
		for _, ownership := range ownerships {
			if len(ownership) == 2 {
				m.AddPolicy("g", "g2", ownership)
			}
		}
	}

	return nil
}

// SavePolicy saves policy rules to database
func (a *DatabaseAdapter) SavePolicy(m model.Model) error {
	if m == nil {
		m = a.model
	}

	// Save policy rules
	policies := m.GetPolicy("p", "p")
	if err := a.db.Set(dbCasbinPrefix, dbPolicyKey, policies); err != nil {
		return fmt.Errorf("failed to save policy: %w", err)
	}

	// Save role assignments
	roles := m.GetPolicy("g", "g")
	if err := a.db.Set(dbCasbinPrefix, dbRoleKey, roles); err != nil {
		return fmt.Errorf("failed to save roles: %w", err)
	}

	// Save resource ownership
	ownerships := m.GetPolicy("g", "g2")
	if err := a.db.Set(dbCasbinPrefix, dbOwnerPrefix, ownerships); err != nil {
		return fmt.Errorf("failed to save ownership: %w", err)
	}

	return nil
}

// AddPolicy adds a policy rule to the storage
func (a *DatabaseAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	if a.model != nil {
		a.model.AddPolicy(sec, ptype, rule)
	}
	return a.SavePolicy(nil)
}

// RemovePolicy removes a policy rule from the storage
func (a *DatabaseAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	if a.model != nil {
		a.model.RemovePolicy(sec, ptype, rule)
	}
	return a.SavePolicy(nil)
}

// RemoveFilteredPolicy removes policy rules that match the filter from the storage
func (a *DatabaseAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	if a.model != nil {
		a.model.RemoveFilteredPolicy(sec, ptype, fieldIndex, fieldValues...)
	}
	return a.SavePolicy(nil)
}
