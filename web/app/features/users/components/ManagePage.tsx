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

import React, { useState, useEffect } from 'react';
import { useAuth } from '../../../contexts/AuthContext';
import { useStreaming } from '../../../contexts/StreamingContext/index';
import { useUsers, useUserCreate, useUserUpdate, useUserRemove } from '../hooks/useUsers';
import { useRoles, useRoleCreate, useRoleUpdate, useRoleRemove } from '../../roles/hooks/useRoles';
import { useUserGroups, useUserGroupCreate, useUserGroupUpdate, useUserGroupRemove } from '../../usergroups/hooks/useUserGroups';
import { StreamingList, type ListColumn, type ListItemAction } from '../../../components/StreamingList';
import { UserForm, RoleForm, UserGroupForm } from '../../../../gen/components';
import { Modal } from '../../../components/Modal';
import type { User, UserGroup } from '../../../../gen/aquarium/v2/user_pb';
import type { Role } from '../../../../gen/aquarium/v2/role_pb';
import { PermService, PermUser, PermRole } from '../../../../gen/permissions/permissions_grpc';

export function ManagePage() {
  const { hasPermission } = useAuth();
  const { fetchUsers, fetchRoles, fetchUserGroups } = useStreaming();
  useUsers();
  useRoles();
  useUserGroups();

  const { create: createUser } = useUserCreate();
  const { update: updateUser } = useUserUpdate();
  const { remove: removeUser } = useUserRemove();

  const { create: createRole } = useRoleCreate();
  const { update: updateRole } = useRoleUpdate();
  const { remove: removeRole } = useRoleRemove();

  const { create: createUserGroup } = useUserGroupCreate();
  const { update: updateUserGroup } = useUserGroupUpdate();
  const { remove: removeUserGroup } = useUserGroupRemove();

  const [activeTab, setActiveTab] = useState<'users' | 'roles' | 'usergroups'>('users');

  // Users state
  const [showCreateUserModal, setShowCreateUserModal] = useState(false);
  const [showUserDetailsModal, setShowUserDetailsModal] = useState(false);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [hasUserCreateChanges, setHasUserCreateChanges] = useState(false);
  const [hasUserEditChanges, setHasUserEditChanges] = useState(false);

  // Roles state
  const [showCreateRoleModal, setShowCreateRoleModal] = useState(false);
  const [showRoleDetailsModal, setShowRoleDetailsModal] = useState(false);
  const [selectedRole, setSelectedRole] = useState<Role | null>(null);
  const [hasRoleCreateChanges, setHasRoleCreateChanges] = useState(false);
  const [hasRoleEditChanges, setHasRoleEditChanges] = useState(false);

  // User Groups state
  const [showCreateUserGroupModal, setShowCreateUserGroupModal] = useState(false);
  const [showUserGroupDetailsModal, setShowUserGroupDetailsModal] = useState(false);
  const [selectedUserGroup, setSelectedUserGroup] = useState<UserGroup | null>(null);
  const [hasUserGroupCreateChanges, setHasUserGroupCreateChanges] = useState(false);
  const [hasUserGroupEditChanges, setHasUserGroupEditChanges] = useState(false);

  // Fetch data when component mounts
  useEffect(() => {
    fetchUsers();
    fetchRoles();
    fetchUserGroups();
  }, [fetchUsers, fetchRoles, fetchUserGroups]);

  // Role operations
  const handleCreateRole = async (roleData: Role) => {
    try {
      console.debug('Creating:', roleData);
      await createRole(roleData);
      setShowCreateRoleModal(false);
    } catch (error) {
      console.error('Failed to create role:', error);
    }
  };

  const handleUpdateRole = async (roleData: Role) => {
    if (!selectedRole) return;
    try {
      console.debug('Updating:', roleData);
      await updateRole(roleData);
      setShowRoleDetailsModal(false);
      setSelectedRole(null);
    } catch (error) {
      console.error('Failed to update role:', error);
    }
  };

  const handleRemoveRole = async (role: Role) => {
    if (!confirm('Are you sure you want to delete this role?')) return;
    try {
      await removeRole(role.name);
    } catch (error) {
      console.error('Failed to delete role:', error);
    }
  };

  // User operations
  const handleCreateUser = async (userData: User) => {
    try {
      console.debug('Creating:', userData);
      await createUser(userData);
      setShowCreateUserModal(false);
    } catch (error) {
      console.error('Failed to create user:', error);
    }
  };

  const handleUpdateUser = async (userData: User) => {
    if (!selectedUser) return;
    try {
      console.debug('Updating:', userData);
      await updateUser(userData);
      setShowUserDetailsModal(false);
      setSelectedUser(null);
    } catch (error) {
      console.error('Failed to update user:', error);
    }
  };

  const handleRemoveUser = async (user: User) => {
    if (!confirm('Are you sure you want to delete this user?')) return;
    try {
      await removeUser(user.name);
    } catch (error) {
      console.error('Failed to delete user:', error);
    }
  };

  // User Group operations
  const handleCreateUserGroup = async (groupData: UserGroup) => {
    try {
      console.debug('Creating:', groupData);
      await createUserGroup(groupData);
      setShowCreateUserGroupModal(false);
    } catch (error) {
      console.error('Failed to create user group:', error);
    }
  };

  const handleUpdateUserGroup = async (groupData: UserGroup) => {
    if (!selectedUserGroup) return;
    try {
      console.debug('Updating:', groupData);
      await updateUserGroup(groupData);
      setShowUserGroupDetailsModal(false);
      setSelectedUserGroup(null);
    } catch (error) {
      console.error('Failed to update user group:', error);
    }
  };

  const handleRemoveUserGroup = async (group: UserGroup) => {
    if (!confirm('Are you sure you want to delete this user group?')) return;
    try {
      await removeUserGroup(group.name);
    } catch (error) {
      console.error('Failed to delete user group:', error);
    }
  };

  // Define columns for users list
  const userColumns: ListColumn[] = [
    {
      key: 'name',
      label: 'User',
      filterable: true,
      render: (user: User) => (
        <div>
          <div className="text-sm font-medium text-gray-900 dark:text-white">
            {user.name}
          </div>
          <div className="text-sm text-gray-500 dark:text-gray-400">
            Roles: {(user.roles || []).join(', ') || 'None'}
          </div>
        </div>
      ),
    },
  ];

  // Define actions for users
  const userActions: ListItemAction[] = [
    {
      label: 'Edit',
      onClick: (user: User) => {
        setSelectedUser(user);
        setShowUserDetailsModal(true);
      },
      className: 'px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200',
      permission: { resource: PermService.User, action: PermUser.Update },
    },
    {
      label: 'Remove',
      onClick: handleRemoveUser,
      className: 'px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200',
      condition: (user: User) => user.name !== 'admin',
      permission: { resource: PermService.User, action: PermUser.Remove },
    },
  ];

  // Define columns for roles list
  const roleColumns: ListColumn[] = [
    {
      key: 'name',
      label: 'Role',
      filterable: true,
      render: (role: Role) => (
        <div>
          <div className="text-sm font-medium text-gray-900 dark:text-white">
            {role.name}
          </div>
          <div className="text-sm text-gray-500 dark:text-gray-400">
            Permissions: {role.permissions?.length || 0}
          </div>
        </div>
      ),
    },
  ];

  // Define actions for roles
  const roleActions: ListItemAction[] = [
    {
      label: 'Edit',
      onClick: (role: Role) => {
        setSelectedRole(role);
        setShowRoleDetailsModal(true);
      },
      className: 'px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200',
      permission: { resource: PermService.Role, action: PermRole.Update },
    },
    {
      label: 'Remove',
      onClick: handleRemoveRole,
      className: 'px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200',
      condition: (role: Role) => role.name !== 'Administrator',
      permission: { resource: PermService.Role, action: PermRole.Remove },
    },
  ];

  // Define columns for user groups list
  const userGroupColumns: ListColumn[] = [
    {
      key: 'name',
      label: 'User Group',
      filterable: true,
      render: (group: UserGroup) => (
        <div>
          <div className="text-sm font-medium text-gray-900 dark:text-white">
            {group.name}
          </div>
          <div className="text-sm text-gray-500 dark:text-gray-400">
            Users: {(group.users || []).length} | Config: {group.config ? 'Yes' : 'No'}
          </div>
        </div>
      ),
    },
  ];

  // Define actions for user groups
  const userGroupActions: ListItemAction[] = [
    {
      label: 'Edit',
      onClick: (group: UserGroup) => {
        setSelectedUserGroup(group);
        setShowUserGroupDetailsModal(true);
      },
      className: 'px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200',
      permission: { resource: PermService.User, action: PermUser.Update },
    },
    {
      label: 'Remove',
      onClick: handleRemoveUserGroup,
      className: 'px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200',
      permission: { resource: PermService.User, action: PermUser.Remove },
    },
  ];

  // Permissions
  const canCreateUser = hasPermission(PermService.User, PermUser.Create);
  const canCreateRole = hasPermission(PermService.Role, PermRole.Create);
  const canCreateUserGroup = hasPermission(PermService.User, PermUser.Create);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
            Management
          </h1>
          <p className="text-gray-600 dark:text-gray-400">
            Manage labels and users
          </p>
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-200 dark:border-gray-700">
        <nav className="flex space-x-8">
          <button
            onClick={() => setActiveTab('users')}
            className={`py-2 px-1 border-b-2 font-medium text-sm ${
              activeTab === 'users'
                ? 'border-blue-500 text-blue-600'
                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
            }`}
          >
            Users
          </button>
          <button
            onClick={() => setActiveTab('roles')}
            className={`py-2 px-1 border-b-2 font-medium text-sm ${
              activeTab === 'roles'
                ? 'border-blue-500 text-blue-600'
                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
            }`}
          >
            Roles
          </button>
          <button
            onClick={() => setActiveTab('usergroups')}
            className={`py-2 px-1 border-b-2 font-medium text-sm ${
              activeTab === 'usergroups'
                ? 'border-blue-500 text-blue-600'
                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
            }`}
          >
            User Groups
          </button>
        </nav>
      </div>

      {/* Users Tab */}
      {activeTab === 'users' && (
        <div className="space-y-4">
          <div className="flex justify-between items-center">
            <div>
              <h2 className="text-lg font-medium text-gray-900 dark:text-white">
                User Management
              </h2>
            </div>
            {canCreateUser && (
              <button
                onClick={() => setShowCreateUserModal(true)}
                className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700"
              >
                Create User
              </button>
            )}
          </div>

          <StreamingList
            objectType="users"
            columns={userColumns}
            actions={userActions}
            filterBy={['name']}
            itemKey={(user: User) => user.name}
            permissions={{ list: { resource: PermService.User, action: PermUser.List } }}
            emptyMessage="No users found"
          />
        </div>
      )}

      {/* Roles Tab */}
      {activeTab === 'roles' && (
        <div className="space-y-4">
          <div className="flex justify-between items-center">
            <div>
              <h2 className="text-lg font-medium text-gray-900 dark:text-white">
                Role Management
              </h2>
            </div>
            {canCreateRole && (
              <button
                onClick={() => setShowCreateRoleModal(true)}
                className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700"
              >
                Create Role
              </button>
            )}
          </div>

          <StreamingList
            objectType="roles"
            columns={roleColumns}
            actions={roleActions}
            filterBy={['name']}
            itemKey={(role: Role) => role.name}
            permissions={{ list: { resource: PermService.Role, action: PermRole.List } }}
            emptyMessage="No roles found"
          />
        </div>
      )}

      {/* User Groups Tab */}
      {activeTab === 'usergroups' && (
        <div className="space-y-4">
          <div className="flex justify-between items-center">
            <div>
              <h2 className="text-lg font-medium text-gray-900 dark:text-white">
                User Group Management
              </h2>
            </div>
            {canCreateUserGroup && (
              <button
                onClick={() => setShowCreateUserGroupModal(true)}
                className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700"
              >
                Create User Group
              </button>
            )}
          </div>

          <StreamingList
            objectType="usergroups"
            columns={userGroupColumns}
            actions={userGroupActions}
            filterBy={['name']}
            itemKey={(group: UserGroup) => group.name}
            permissions={{ list: { resource: PermService.User, action: PermUser.List } }}
            emptyMessage="No user groups found"
          />
        </div>
      )}

      {/* Create Role Modal */}
      <Modal
        isOpen={showCreateRoleModal}
        onClose={() => setShowCreateRoleModal(false)}
        hasUnsavedChanges={hasRoleCreateChanges}
      >
        <RoleForm
          mode="create"
          onSubmit={handleCreateRole}
          onCancel={() => setShowCreateRoleModal(false)}
          onFormChange={setHasRoleCreateChanges}
          title="Create Role"
        />
      </Modal>

      {/* Edit Role Modal */}
      {selectedRole && (
        <Modal
          isOpen={showRoleDetailsModal}
          onClose={() => {
            setShowRoleDetailsModal(false);
            setSelectedRole(null);
          }}
          hasUnsavedChanges={hasRoleEditChanges}
        >
          <RoleForm
            mode="edit"
            initialData={selectedRole}
            onSubmit={handleUpdateRole}
            onCancel={() => {
              setShowRoleDetailsModal(false);
              setSelectedRole(null);
            }}
            onFormChange={setHasRoleEditChanges}
            title={`Edit Role: ${selectedRole.name}`}
          />
        </Modal>
      )}

      {/* Create User Modal */}
      <Modal
        isOpen={showCreateUserModal}
        onClose={() => setShowCreateUserModal(false)}
        hasUnsavedChanges={hasUserCreateChanges}
      >
        <UserForm
          mode="create"
          onSubmit={handleCreateUser}
          onCancel={() => setShowCreateUserModal(false)}
          onFormChange={setHasUserCreateChanges}
          title="Create User"
        />
      </Modal>

      {/* Edit User Modal */}
      {selectedUser && (
        <Modal
          isOpen={showUserDetailsModal}
          onClose={() => {
            setShowUserDetailsModal(false);
            setSelectedUser(null);
          }}
          hasUnsavedChanges={hasUserEditChanges}
        >
          <UserForm
            mode="edit"
            initialData={selectedUser}
            onSubmit={handleUpdateUser}
            onCancel={() => {
              setShowUserDetailsModal(false);
              setSelectedUser(null);
            }}
            onFormChange={setHasUserEditChanges}
            title={`Edit User: ${selectedUser.name}`}
          />
        </Modal>
      )}

      {/* Create User Group Modal */}
      <Modal
        isOpen={showCreateUserGroupModal}
        onClose={() => setShowCreateUserGroupModal(false)}
        hasUnsavedChanges={hasUserGroupCreateChanges}
      >
        <UserGroupForm
          mode="create"
          onSubmit={handleCreateUserGroup}
          onCancel={() => setShowCreateUserGroupModal(false)}
          onFormChange={setHasUserGroupCreateChanges}
          title="Create User Group"
        />
      </Modal>

      {/* Edit User Group Modal */}
      {selectedUserGroup && (
        <Modal
          isOpen={showUserGroupDetailsModal}
          onClose={() => {
            setShowUserGroupDetailsModal(false);
            setSelectedUserGroup(null);
          }}
          hasUnsavedChanges={hasUserGroupEditChanges}
        >
          <UserGroupForm
            mode="edit"
            initialData={selectedUserGroup}
            onSubmit={handleUpdateUserGroup}
            onCancel={() => {
              setShowUserGroupDetailsModal(false);
              setSelectedUserGroup(null);
            }}
            onFormChange={setHasUserGroupEditChanges}
            title={`Edit User Group: ${selectedUserGroup.name}`}
          />
        </Modal>
      )}
    </div>
  );
}

