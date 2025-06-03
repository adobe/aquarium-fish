package auth

import (
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Action types
const (
	// Regular actions to get access to the resource
	ActionList   = "list"
	ActionCreate = "create"
	ActionRead   = "read"
	ActionUpdate = "update"
	ActionDelete = "delete"

	// For admin use to get access on all the resources
	ActionReadAll   = "read_all"
	ActionUpdateAll = "update_all"
	ActionListAll   = "list_all"
	ActionDeleteAll = "delete_all"

	// Special actions
	ActionAssignRole    = "assign_role"    // Update User role
	ActionAccess        = "access"         // ApplicationResource credentials for SSH access
	ActionDeallocate    = "deallocate"     // Application action to deallocate
	ActionDeallocateAll = "deallocate_all" // Application action to deallocate by Administrator
	ActionMaintainance  = "maintenance"    // Controlling node maintenance state
	ActionProfiling     = "profiling"      // Access to node profiling data
)

// All available permissions
var allPermissions = []types.Permission{
	{Resource: types.ObjectApplication, Action: ActionCreate},
	{Resource: types.ObjectApplication, Action: ActionDeallocate},
	{Resource: types.ObjectApplication, Action: ActionDeallocateAll},
	{Resource: types.ObjectApplication, Action: ActionList},
	{Resource: types.ObjectApplication, Action: ActionListAll},
	{Resource: types.ObjectApplication, Action: ActionRead},
	{Resource: types.ObjectApplication, Action: ActionReadAll},

	{Resource: types.ObjectApplicationResource, Action: ActionAccess},
	{Resource: types.ObjectApplicationResource, Action: ActionRead},
	{Resource: types.ObjectApplicationResource, Action: ActionReadAll},

	{Resource: types.ObjectApplicationState, Action: ActionRead},
	{Resource: types.ObjectApplicationState, Action: ActionReadAll},

	{Resource: types.ObjectApplicationTask, Action: ActionCreate},
	{Resource: types.ObjectApplicationTask, Action: ActionList},
	{Resource: types.ObjectApplicationTask, Action: ActionListAll},
	{Resource: types.ObjectApplicationTask, Action: ActionRead},
	{Resource: types.ObjectApplicationTask, Action: ActionReadAll},

	{Resource: types.ObjectLabel, Action: ActionCreate},
	{Resource: types.ObjectLabel, Action: ActionRead},
	{Resource: types.ObjectLabel, Action: ActionList},
	{Resource: types.ObjectLabel, Action: ActionDelete},

	{Resource: types.ObjectNode, Action: ActionList},
	{Resource: types.ObjectNode, Action: ActionRead},
	{Resource: types.ObjectNode, Action: ActionMaintainance},
	{Resource: types.ObjectNode, Action: ActionProfiling},

	{Resource: types.ObjectRole, Action: ActionCreate},
	{Resource: types.ObjectRole, Action: ActionDelete},
	{Resource: types.ObjectRole, Action: ActionList},
	{Resource: types.ObjectRole, Action: ActionRead},
	{Resource: types.ObjectRole, Action: ActionUpdate},

	{Resource: types.ObjectUser, Action: ActionAssignRole},
	{Resource: types.ObjectUser, Action: ActionCreate},
	{Resource: types.ObjectUser, Action: ActionDelete},
	{Resource: types.ObjectUser, Action: ActionList},
	{Resource: types.ObjectUser, Action: ActionRead},
	{Resource: types.ObjectUser, Action: ActionReadAll},
	{Resource: types.ObjectUser, Action: ActionUpdate},
	{Resource: types.ObjectUser, Action: ActionUpdateAll},
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
		{Resource: types.ObjectApplication, Action: ActionDeallocate},
		{Resource: types.ObjectApplication, Action: ActionList},
		{Resource: types.ObjectApplication, Action: ActionRead},

		{Resource: types.ObjectApplicationResource, Action: ActionRead},

		{Resource: types.ObjectApplicationState, Action: ActionRead},

		{Resource: types.ObjectLabel, Action: ActionList},
	}
}

// GetPowerPermissions returns a list of power User permissions
// They give access to ApplicationTask & ApplicationResource access requests
func GetPowerPermissions() []types.Permission {
	return []types.Permission{
		{Resource: types.ObjectApplicationTask, Action: ActionCreate},
		{Resource: types.ObjectApplicationTask, Action: ActionList},
		{Resource: types.ObjectApplicationTask, Action: ActionRead},

		{Resource: types.ObjectApplicationResource, Action: ActionAccess},
	}
}
