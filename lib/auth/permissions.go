package auth

import (
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Action types
const (
	ActionList    = "list"
	ActionListAll = "list_all"
	ActionCreate  = "create"
	ActionRead    = "read"
	ActionUpdate  = "update"
	ActionDelete  = "delete"

	// Special actions
	ActionAccess       = "access"      // ApplicationResource credentials for SSH access
	ActionDeallocate   = "deallocate"  // Application action to deallocate
	ActionMaintainance = "maintenance" // Controlling node maintenance state
	ActionProfiling    = "profiling"   // Access to node profiling data
)

// All available permissions
var allPermissions = []types.Permission{
	{Resource: types.ObjectApplication, Action: ActionCreate},
	{Resource: types.ObjectApplication, Action: ActionDeallocate},
	{Resource: types.ObjectApplication, Action: ActionListAll},
	{Resource: types.ObjectApplication, Action: ActionRead},

	{Resource: types.ObjectApplicationResource, Action: ActionAccess},
	{Resource: types.ObjectApplicationResource, Action: ActionRead},

	{Resource: types.ObjectApplicationState, Action: ActionRead},

	{Resource: types.ObjectApplicationTask, Action: ActionCreate},
	{Resource: types.ObjectApplicationTask, Action: ActionList},
	{Resource: types.ObjectApplicationTask, Action: ActionRead},

	{Resource: types.ObjectLabel, Action: ActionCreate},
	{Resource: types.ObjectLabel, Action: ActionRead},
	{Resource: types.ObjectLabel, Action: ActionList},
	{Resource: types.ObjectLabel, Action: ActionDelete},

	{Resource: types.ObjectNode, Action: ActionList},
	{Resource: types.ObjectNode, Action: ActionMaintainance},
	{Resource: types.ObjectNode, Action: ActionProfiling},

	{Resource: types.ObjectRole, Action: ActionCreate},
	{Resource: types.ObjectRole, Action: ActionDelete},
	{Resource: types.ObjectRole, Action: ActionList},
	{Resource: types.ObjectRole, Action: ActionRead},
	{Resource: types.ObjectRole, Action: ActionUpdate},

	{Resource: types.ObjectUser, Action: ActionCreate},
	{Resource: types.ObjectUser, Action: ActionDelete},
	{Resource: types.ObjectUser, Action: ActionList},
	{Resource: types.ObjectUser, Action: ActionRead},
	{Resource: types.ObjectUser, Action: ActionUpdate},
}

// GetAllPermissions returns a list of all possible (admin) permission combinations
func GetAllPermissions() []types.Permission {
	return allPermissions
}

// GetUserPermissions returns a list of regular User permissions
// User permissions allows just to create new Application and list Labels, other actions are
// allowed by ownership (owner of the Object can control it), so we need a little here for User.
func GetUserPermissions() []types.Permission {
	return []types.Permission{
		{Resource: types.ObjectApplication, Action: ActionCreate},
		{Resource: types.ObjectLabel, Action: ActionList},
	}
}
