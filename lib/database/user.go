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

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func (d *Database) subscribeUserImpl(_ context.Context, ch chan UserSubscriptionEvent) {
	subscribeHelper(d, &d.subsUser, ch)
}

// unsubscribeUserImpl removes a channel from the subscription list
func (d *Database) unsubscribeUserImpl(_ context.Context, ch chan UserSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsUser, ch)
}

// UserGroup subscription methods

func (d *Database) subscribeUserGroupImpl(_ context.Context, ch chan UserGroupSubscriptionEvent) {
	subscribeHelper(d, &d.subsUserGroup, ch)
}

// unsubscribeUserGroupImpl removes a channel from the subscription list
func (d *Database) unsubscribeUserGroupImpl(_ context.Context, ch chan UserGroupSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsUserGroup, ch)
}

// userListImpl returns list of users
func (d *Database) userListImpl(_ context.Context) (us []typesv2.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).List(&us)
	return us, err
}

// userCreateImpl makes new User
func (d *Database) userCreateImpl(_ context.Context, u *typesv2.User) error {
	if u.Name == "" {
		return fmt.Errorf("user name can't be empty")
	}
	if hash, err := u.GetHash(); err != nil || hash.IsEmpty() {
		return fmt.Errorf("user hash can't be empty, err: %v", err)
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	err := d.be.Collection(ObjectUser).Add(u.Name, u)

	if err == nil {
		// Notify subscribers about the new User
		notifySubscribersHelper(d, &d.subsUser, NewCreateEvent(u), ObjectUser)
	}

	return err
}

// userSaveImpl stores User
func (d *Database) userSaveImpl(_ context.Context, u *typesv2.User) error {
	u.UpdatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectUser).Add(u.Name, u)
	d.beMu.RUnlock()

	if err == nil {
		// Notify subscribers about the updated User
		notifySubscribersHelper(d, &d.subsUser, NewUpdateEvent(u), ObjectUser)
	}

	return err
}

// userGetImpl returns User by unique name
func (d *Database) userGetImpl(_ context.Context, name string) (u *typesv2.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).Get(name, &u)
	return u, err
}

// userDeleteImpl removes User
func (d *Database) userDeleteImpl(ctx context.Context, name string) error {
	// Get the object before deleting it for notification
	u, getErr := d.UserGet(ctx, name)
	if getErr != nil {
		return getErr
	}

	d.beMu.RLock()
	err := d.be.Collection(ObjectUser).Delete(name)
	d.beMu.RUnlock()

	if err == nil && u != nil {
		// Notify subscribers about the removed User
		notifySubscribersHelper(d, &d.subsUser, NewRemoveEvent(u), ObjectUser)
	}

	return err
}

// userAuthImpl returns User if name and password are correct
func (d *Database) userAuthImpl(ctx context.Context, name string, password string) *typesv2.User {
	// TODO: Make auth process to take constant time in case of failure
	user, err := d.UserGet(ctx, name)
	if err != nil {
		log.WithFunc("database", "userAuthImpl").WarnContext(ctx, "User does not exists", "name", name)
		return nil
	}

	if hash, err := user.GetHash(); err != nil || !hash.IsEqual(password) {
		log.WithFunc("database", "userAuthImpl").WarnContext(ctx, "Incorrect user password", "name", name, "err", err)
		return nil
	}

	return user
}

// userNewImpl makes new User with defined or generated password
func (*Database) userNewImpl(ctx context.Context, name string, password string) (string, *typesv2.User, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &typesv2.User{
		Name: name,
	}
	if err := user.SetHash(crypt.NewHash(password, nil)); err != nil {
		log.WithFunc("database", "userNewImpl").ErrorContext(ctx, "Unable to set hash for new user", "name", name, "err", err)
		return "", nil, fmt.Errorf("unable to set hash for new user %q: %v", name, err)
	}

	return password, user, nil
}

// UserGroup database operations

// userGroupListImpl returns list of user groups
func (d *Database) userGroupListImpl(_ context.Context) (groups []typesv2.UserGroup, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUserGroup).List(&groups)
	return groups, err
}

// userGroupCreateImpl makes new UserGroup
func (d *Database) userGroupCreateImpl(_ context.Context, g *typesv2.UserGroup) error {
	if g.Name == "" {
		return fmt.Errorf("user group name can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	g.CreatedAt = time.Now()
	g.UpdatedAt = g.CreatedAt
	err := d.be.Collection(ObjectUserGroup).Add(g.Name, g)

	if err == nil {
		// Notify subscribers about the new UserGroup
		notifySubscribersHelper(d, &d.subsUserGroup, NewCreateEvent(g), ObjectUserGroup)
	}

	return err
}

// userGroupSaveImpl stores UserGroup
func (d *Database) userGroupSaveImpl(_ context.Context, g *typesv2.UserGroup) error {
	g.UpdatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectUserGroup).Add(g.Name, g)
	d.beMu.RUnlock()

	if err == nil {
		// Notify subscribers about the updated UserGroup
		notifySubscribersHelper(d, &d.subsUserGroup, NewUpdateEvent(g), ObjectUserGroup)
	}

	return err
}

// userGroupGetImpl returns UserGroup by unique name
func (d *Database) userGroupGetImpl(_ context.Context, name string) (g *typesv2.UserGroup, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUserGroup).Get(name, &g)
	return g, err
}

// userGroupDeleteImpl removes UserGroup
func (d *Database) userGroupDeleteImpl(ctx context.Context, name string) error {
	// Get the object before deleting it for notification
	g, getErr := d.UserGroupGet(ctx, name)
	if getErr != nil {
		return getErr
	}

	d.beMu.RLock()
	err := d.be.Collection(ObjectUserGroup).Delete(name)
	d.beMu.RUnlock()

	if err == nil && g != nil {
		// Notify subscribers about the removed UserGroup
		notifySubscribersHelper(d, &d.subsUserGroup, NewRemoveEvent(g), ObjectUserGroup)
	}

	return err
}

// GetUserGroups Finds all groups of the user
func (d *Database) userGroupListByUserImpl(ctx context.Context, userName string) ([]*typesv2.UserGroup, error) {
	// Get all user groups and find the ones this user belongs to
	var userGroups []*typesv2.UserGroup
	groups, err := d.UserGroupList(ctx)
	if err != nil {
		return userGroups, err
	}

	// Collect configs from groups that contain this user
	for _, group := range groups {
		// Check if user is in this group
		userInGroup := false
		for _, groupUser := range group.Users {
			if groupUser == userName {
				userInGroup = true
				break
			}
		}

		if userInGroup {
			userGroups = append(userGroups, &group)
		}
	}

	return userGroups, nil
}

// MergeUserConfigWithGroups merges user configuration with user group configurations
// Returns a new UserConfig that uses user's config values if set, otherwise uses the
// maximum value from all user groups' configs, and finally falls back to defaults
func (*Database) mergeUserConfigWithGroupsImpl(_ context.Context, user *typesv2.User, groups []*typesv2.UserGroup) *typesv2.UserConfig {
	// Start with user's existing config or create a new one
	mergedConfig := &typesv2.UserConfig{}
	if user.Config != nil {
		// Copy user's config
		if user.Config.RateLimit != nil {
			val := *user.Config.RateLimit
			mergedConfig.RateLimit = &val
		}
		if user.Config.StreamsLimit != nil {
			val := *user.Config.StreamsLimit
			mergedConfig.StreamsLimit = &val
		}
	}

	// Collect configs from groups that contain this user
	var groupConfigs []*typesv2.UserConfig
	for _, group := range groups {
		if group.Config != nil {
			groupConfigs = append(groupConfigs, group.Config)
		}
	}

	if len(groupConfigs) == 0 {
		return mergedConfig
	}

	// Merge RateLimit: use user's value if set, otherwise use max from groups
	if mergedConfig.RateLimit == nil {
		var maxRateLimit *int32
		for _, gc := range groupConfigs {
			if gc.RateLimit != nil {
				if maxRateLimit == nil || *gc.RateLimit > *maxRateLimit {
					val := *gc.RateLimit
					maxRateLimit = &val
				}
			}
		}
		if maxRateLimit != nil {
			mergedConfig.RateLimit = maxRateLimit
		}
	}

	// Merge StreamsLimit: use user's value if set, otherwise use max from groups
	if mergedConfig.StreamsLimit == nil {
		var maxStreamsLimit *int32
		for _, gc := range groupConfigs {
			if gc.StreamsLimit != nil {
				if maxStreamsLimit == nil || *gc.StreamsLimit > *maxStreamsLimit {
					val := *gc.StreamsLimit
					maxStreamsLimit = &val
				}
			}
		}
		if maxStreamsLimit != nil {
			mergedConfig.StreamsLimit = maxStreamsLimit
		}
	}

	return mergedConfig
}

// EnrichUserWithGroupConfig enriches a user object with merged configuration from user groups
// This modifies the user object in place
func (d *Database) enrichUserWithGroupConfigImpl(ctx context.Context, user *typesv2.User) {
	if user == nil {
		return
	}

	userGroups, err := d.UserGroupListByUser(ctx, user.Name)
	if err != nil {
		log.WithFunc("database", "EnrichUserWithGroupConfig").Error("Unable to get ")
		return
	}
	if len(userGroups) > 0 {
		// Set user groups list
		var groupNames []string
		for _, group := range userGroups {
			groupNames = append(groupNames, group.Name)
		}
		user.SetGroups(groupNames)

		// Merge user config with the groups
		mergedConfig := d.MergeUserConfigWithGroups(ctx, user, userGroups)

		// Only update if we got some config values from groups
		if mergedConfig.RateLimit != nil || mergedConfig.StreamsLimit != nil {
			if user.Config == nil {
				user.Config = mergedConfig
			} else {
				// Merge into existing config
				if user.Config.RateLimit == nil && mergedConfig.RateLimit != nil {
					user.Config.RateLimit = mergedConfig.RateLimit
				}
				if user.Config.StreamsLimit == nil && mergedConfig.StreamsLimit != nil {
					user.Config.StreamsLimit = mergedConfig.StreamsLimit
				}
			}
		}
	}
}
