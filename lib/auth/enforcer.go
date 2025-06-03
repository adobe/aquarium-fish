package auth

import (
	"embed"
	"fmt"
	"sync"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

//go:embed model.conf
var modelFS embed.FS

// Enforcer wraps Casbin enforcer with additional functionality
type Enforcer struct {
	enforcer *casbin.Enforcer
	adapter  persist.Adapter
	mu       sync.RWMutex
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

	// Set as global enforcer if not already set
	if globalEnforcer == nil {
		SetEnforcer(e)
	}

	return e, nil
}

// CheckPermission checks if the roles has permission to perform the action on the object
func (e *Enforcer) CheckPermission(roles []string, obj, act string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	log.Debug("Auth: Enforcer:", roles, obj, act)

	for _, role := range roles {
		allowed, err := e.enforcer.Enforce(role, obj, act)
		if err != nil {
			log.Errorf("Failed to check permission: %v", err)
			return false
		}
		if allowed {
			return true
		}
	}
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

// SavePolicy saves all policy rules to storage
func (e *Enforcer) SavePolicy() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.enforcer.SavePolicy()
}
