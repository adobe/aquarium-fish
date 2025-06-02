package database

import (
	"fmt"
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// RoleList returns a list of all roles
func (d *Database) RoleList() (rs []types.Role, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(types.ObjectRole).List(&rs)
	return rs, err
}

// RoleGet returns a role by name
func (d *Database) RoleGet(name string) (r *types.Role, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(types.ObjectRole).Get(name, &r)
	return r, err
}

// RoleCreate makes a new role
func (d *Database) RoleCreate(r *types.Role) error {
	if r.Name == "" {
		return fmt.Errorf("Fish: Role.Name can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	return d.be.Collection(types.ObjectRole).Add(r.Name, r)
}

// RoleSave saves a role
func (d *Database) RoleSave(r *types.Role) error {
	if r.Name == "" {
		return fmt.Errorf("Fish: Role.Name can't be empty")
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("Fish: Role.CreatedAt can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	r.UpdatedAt = time.Now()
	return d.be.Collection(types.ObjectRole).Add(r.Name, r)
}

// RoleDelete deletes a role
func (d *Database) RoleDelete(name string) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(types.ObjectRole).Delete(name)
}
