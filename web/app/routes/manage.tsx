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
import { DashboardLayout } from '../components/DashboardLayout';
import { ProtectedRoute } from '../components/ProtectedRoute';
import { useAuth } from '../contexts/AuthContext';
import { labelServiceHelpers, userServiceHelpers, roleServiceHelpers } from '../lib/services';
import type { Label } from '../../gen/aquarium/v2/label_pb';
import type { User } from '../../gen/aquarium/v2/user_pb';
import type { Role } from '../../gen/aquarium/v2/role_pb';
import * as yaml from 'js-yaml';

export function meta() {
  return [
    { title: 'Management - Aquarium Fish' },
    { name: 'description', content: 'Manage labels and users' },
  ];
}

export default function Manage() {
  const { user, hasPermission } = useAuth();
  const [activeTab, setActiveTab] = useState<'labels' | 'users'>('labels');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Labels state
  const [labels, setLabels] = useState<Label[]>([]);
  const [showCreateLabelModal, setShowCreateLabelModal] = useState(false);
  const [showLabelDetailsModal, setShowLabelDetailsModal] = useState(false);
  const [selectedLabel, setSelectedLabel] = useState<Label | null>(null);
  const [labelYaml, setLabelYaml] = useState('');
  const [labelYamlError, setLabelYamlError] = useState<string | null>(null);
  const [labelNameFilter, setLabelNameFilter] = useState('');
  const [labelVersionFilter, setLabelVersionFilter] = useState('');

  // Users state
  const [users, setUsers] = useState<User[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [showCreateUserModal, setShowCreateUserModal] = useState(false);
  const [showUserDetailsModal, setShowUserDetailsModal] = useState(false);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [userForm, setUserForm] = useState({
    name: '',
    password: '',
    roles: [] as string[],
  });
  const [userFormError, setUserFormError] = useState<string | null>(null);

  // Fetch data
  const fetchData = async () => {
    try {
      setError(null);
      setLoading(true);

      const promises = [];

      if (hasPermission('LabelService', 'List')) {
        promises.push(labelServiceHelpers.list());
      }

      if (hasPermission('UserService', 'List')) {
        promises.push(userServiceHelpers.list());
      }

      if (hasPermission('RoleService', 'List')) {
        promises.push(roleServiceHelpers.list());
      }

      const results = await Promise.all(promises);
      let resultIndex = 0;

      if (hasPermission('LabelService', 'List')) {
        setLabels(results[resultIndex++] || []);
      }

      if (hasPermission('UserService', 'List')) {
        setUsers(results[resultIndex++] || []);
      }

      if (hasPermission('RoleService', 'List')) {
        setRoles(results[resultIndex++] || []);
      }

      setLoading(false);
    } catch (err) {
      setError(`Failed to fetch data: ${err}`);
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  // Format timestamp
  const formatTimestamp = (timestamp: any) => {
    if (!timestamp) return 'Unknown';
    const date = new Date(Number(timestamp.seconds) * 1000);
    return date.toLocaleString();
  };

    // Label operations
  const handleCreateLabel = async () => {
    try {
      setLabelYamlError(null);

      const labelData = yaml.load(labelYaml) as any;

      if (!labelData.name) {
        throw new Error('Label name is required');
      }

      if (!labelData.version) {
        labelData.version = 1;
      }

      if (!labelData.definitions || !Array.isArray(labelData.definitions)) {
        throw new Error('Label definitions are required');
      }

      // Create label object (simplified to avoid protobuf issues)
      const labelObj = {
        uid: crypto.randomUUID(),
        name: labelData.name,
        version: labelData.version,
        definitions: labelData.definitions,
        metadata: labelData.metadata || {},
      };

      console.log('Creating label:', labelObj);
      // TODO: Fix service call when protobuf issues are resolved
      // await labelServiceHelpers.create(labelObj);
      await fetchData(); // Refresh data
      setShowCreateLabelModal(false);
      setLabelYaml('');
    } catch (error) {
      setLabelYamlError(`Failed to create label: ${error}`);
    }
  };

  const handleDeleteLabel = async (uid: string) => {
    if (!confirm('Are you sure you want to delete this label?')) return;

    try {
      await labelServiceHelpers.delete(uid);
      await fetchData(); // Refresh data
    } catch (error) {
      setError(`Failed to delete label: ${error}`);
    }
  };

  // User operations
  const handleCreateUser = async () => {
    try {
      setUserFormError(null);

      if (!userForm.name) {
        throw new Error('User name is required');
      }

      if (!userForm.password) {
        throw new Error('Password is required');
      }

      const userObj = {
        name: userForm.name,
        password: userForm.password,
        roles: userForm.roles,
      };

      await userServiceHelpers.create(userObj);
      await fetchData(); // Refresh data
      setShowCreateUserModal(false);
      setUserForm({ name: '', password: '', roles: [] });
    } catch (error) {
      setUserFormError(`Failed to create user: ${error}`);
    }
  };

  const handleUpdateUser = async () => {
    if (!selectedUser) return;

    try {
      setUserFormError(null);

      const userObj = {
        ...selectedUser,
        roles: userForm.roles,
      };

      if (userForm.password) {
        userObj.password = userForm.password;
      }

      await userServiceHelpers.update(userObj);
      await fetchData(); // Refresh data
      setShowUserDetailsModal(false);
      setSelectedUser(null);
      setUserForm({ name: '', password: '', roles: [] });
    } catch (error) {
      setUserFormError(`Failed to update user: ${error}`);
    }
  };

  const handleDeleteUser = async (userName: string) => {
    if (!confirm('Are you sure you want to delete this user?')) return;

    try {
      await userServiceHelpers.delete(userName);
      await fetchData(); // Refresh data
    } catch (error) {
      setError(`Failed to delete user: ${error}`);
    }
  };

  // Filter labels
  const filteredLabels = labels.filter(label => {
    if (labelNameFilter && !label.name.includes(labelNameFilter)) return false;
    if (labelVersionFilter && labelVersionFilter !== 'all') {
      if (labelVersionFilter === 'latest') {
        // Show only latest version of each label
        const latestVersion = Math.max(...labels.filter(l => l.name === label.name).map(l => l.version));
        return label.version === latestVersion;
      } else {
        return label.version.toString() === labelVersionFilter;
      }
    }
    return true;
  });

  // Group labels by name for version display
  const labelsByName = labels.reduce((acc, label) => {
    if (!acc[label.name]) {
      acc[label.name] = [];
    }
    acc[label.name].push(label);
    return acc;
  }, {} as Record<string, Label[]>);

  // Permissions
  const canCreateLabel = hasPermission('LabelService', 'Create');
  const canDeleteLabel = hasPermission('LabelService', 'Delete');
  const canListLabels = hasPermission('LabelService', 'List');
  const canCreateUser = hasPermission('UserService', 'Create');
  const canUpdateUser = hasPermission('UserService', 'Update');
  const canDeleteUser = hasPermission('UserService', 'Delete');
  const canListUsers = hasPermission('UserService', 'List');

  return (
    <ProtectedRoute>
      <DashboardLayout>
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
            <button
              onClick={fetchData}
              className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700"
            >
              Refresh
            </button>
          </div>

          {/* Tabs */}
          <div className="border-b border-gray-200 dark:border-gray-700">
            <nav className="flex space-x-8">
              <button
                onClick={() => setActiveTab('labels')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  activeTab === 'labels'
                    ? 'border-blue-500 text-blue-600'
                    : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
                }`}
              >
                Labels
              </button>
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
            </nav>
          </div>

          {/* Error */}
          {error && (
            <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-md">
              {error}
            </div>
          )}

          {/* Labels Tab */}
          {activeTab === 'labels' && (
            <div className="space-y-4">
              <div className="flex justify-between items-center">
                <div className="flex items-center space-x-4">
                  <input
                    type="text"
                    placeholder="Filter by name"
                    value={labelNameFilter}
                    onChange={(e) => setLabelNameFilter(e.target.value)}
                    className="px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600"
                  />
                  <select
                    value={labelVersionFilter}
                    onChange={(e) => setLabelVersionFilter(e.target.value)}
                    className="px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600"
                  >
                    <option value="all">All Versions</option>
                    <option value="latest">Latest Only</option>
                  </select>
                </div>
                {canCreateLabel && (
                  <button
                    onClick={() => setShowCreateLabelModal(true)}
                    className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700"
                  >
                    Create Label
                  </button>
                )}
              </div>

              {!canListLabels ? (
                <div className="text-center py-8">
                  <p className="text-red-600 dark:text-red-400">
                    You don't have permission to view labels.
                  </p>
                  <p className="text-gray-600 dark:text-gray-400 mt-2">
                    Required permission: LabelService.List
                  </p>
                </div>
              ) : loading ? (
                <div className="text-center py-8">
                  <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
                  <p className="mt-2 text-gray-600 dark:text-gray-400">Loading labels...</p>
                </div>
              ) : filteredLabels.length === 0 ? (
                <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                  No labels found
                </div>
              ) : (
                <div className="bg-white dark:bg-gray-800 shadow overflow-hidden sm:rounded-md">
                  <ul className="divide-y divide-gray-200 dark:divide-gray-700">
                    {filteredLabels.map((label) => (
                      <li
                        key={label.uid}
                        className="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-700"
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center space-x-4">
                            <div>
                              <div className="text-sm font-medium text-gray-900 dark:text-white">
                                {label.name} v{label.version}
                              </div>
                              <div className="text-sm text-gray-500 dark:text-gray-400">
                                Created: {formatTimestamp(label.createdAt)}
                              </div>
                            </div>
                          </div>
                          <div className="flex items-center space-x-2">
                            <button
                              onClick={() => {
                                setSelectedLabel(label);
                                setShowLabelDetailsModal(true);
                              }}
                              className="px-3 py-1 text-sm bg-blue-100 text-blue-800 rounded-md hover:bg-blue-200"
                            >
                              View Details
                            </button>
                            {canDeleteLabel && (
                              <button
                                onClick={() => handleDeleteLabel(label.uid)}
                                className="px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200"
                              >
                                Delete
                              </button>
                            )}
                          </div>
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

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

              {!canListUsers ? (
                <div className="text-center py-8">
                  <p className="text-red-600 dark:text-red-400">
                    You don't have permission to view users.
                  </p>
                  <p className="text-gray-600 dark:text-gray-400 mt-2">
                    Required permission: UserService.List
                  </p>
                </div>
              ) : loading ? (
                <div className="text-center py-8">
                  <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
                  <p className="mt-2 text-gray-600 dark:text-gray-400">Loading users...</p>
                </div>
              ) : users.length === 0 ? (
                <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                  No users found
                </div>
              ) : (
                <div className="bg-white dark:bg-gray-800 shadow overflow-hidden sm:rounded-md">
                  <ul className="divide-y divide-gray-200 dark:divide-gray-700">
                    {users.map((user) => (
                      <li
                        key={user.name}
                        className="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-700"
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center space-x-4">
                            <div>
                              <div className="text-sm font-medium text-gray-900 dark:text-white">
                                {user.name}
                              </div>
                              <div className="text-sm text-gray-500 dark:text-gray-400">
                                Roles: {user.roles.join(', ') || 'None'}
                              </div>
                            </div>
                          </div>
                          <div className="flex items-center space-x-2">
                            {canUpdateUser && (
                              <button
                                onClick={() => {
                                  setSelectedUser(user);
                                  setUserForm({
                                    name: user.name,
                                    password: '',
                                    roles: user.roles,
                                  });
                                  setShowUserDetailsModal(true);
                                }}
                                className="px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200"
                              >
                                Edit
                              </button>
                            )}
                            {canDeleteUser && user.name !== 'admin' && (
                              <button
                                onClick={() => handleDeleteUser(user.name)}
                                className="px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200"
                              >
                                Delete
                              </button>
                            )}
                          </div>
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Create Label Modal */}
        {showCreateLabelModal && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-3xl max-h-[90vh] overflow-y-auto">
              <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-white">
                Create Label
              </h2>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Label Configuration (YAML)
                  </label>
                  <textarea
                    value={labelYaml}
                    onChange={(e) => setLabelYaml(e.target.value)}
                    className="w-full h-96 px-3 py-2 border border-gray-300 rounded-md font-mono text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                    placeholder={`name: my-label
version: 1
definitions:
  - driver: docker
    options:
      image: ubuntu:20.04
    resources:
      cpu: 2
      ram: 4
      disks: {}
      network: bridge
metadata:
  description: My custom label`}
                  />
                </div>
                {labelYamlError && (
                  <div className="text-sm text-red-600 dark:text-red-400">
                    {labelYamlError}
                  </div>
                )}
                <div className="flex justify-end space-x-3">
                  <button
                    onClick={() => {
                      setShowCreateLabelModal(false);
                      setLabelYaml('');
                      setLabelYamlError(null);
                    }}
                    className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleCreateLabel}
                    className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700"
                  >
                    Create
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Label Details Modal */}
        {showLabelDetailsModal && selectedLabel && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
              <div className="flex justify-between items-center mb-4">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                  Label Details: {selectedLabel.name} v{selectedLabel.version}
                </h2>
                <button
                  onClick={() => setShowLabelDetailsModal(false)}
                  className="text-gray-500 hover:text-gray-700"
                >
                  ×
                </button>
              </div>

              <div className="space-y-6">
                <div>
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                    Basic Information
                  </h3>
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">UID</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedLabel.uid}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Name</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedLabel.name}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Version</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedLabel.version}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Created</p>
                      <p className="text-sm text-gray-900 dark:text-white">
                        {formatTimestamp(selectedLabel.createdAt)}
                      </p>
                    </div>
                  </div>
                </div>

                <div>
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                    Definitions
                  </h3>
                  <pre className="bg-gray-100 dark:bg-gray-700 p-4 rounded-md text-sm overflow-x-auto">
                    {JSON.stringify(selectedLabel.definitions, null, 2)}
                  </pre>
                </div>

                {selectedLabel.metadata && (
                  <div>
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                      Metadata
                    </h3>
                    <pre className="bg-gray-100 dark:bg-gray-700 p-4 rounded-md text-sm overflow-x-auto">
                      {JSON.stringify(selectedLabel.metadata, null, 2)}
                    </pre>
                  </div>
                )}

                {/* Other versions */}
                {labelsByName[selectedLabel.name] && labelsByName[selectedLabel.name].length > 1 && (
                  <div>
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                      Other Versions
                    </h3>
                    <div className="space-y-2">
                      {labelsByName[selectedLabel.name]
                        .filter(l => l.uid !== selectedLabel.uid)
                        .map((label) => (
                          <div
                            key={label.uid}
                            className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-700 rounded-md"
                          >
                            <div>
                              <span className="text-sm font-medium text-gray-900 dark:text-white">
                                Version {label.version}
                              </span>
                              <span className="text-sm text-gray-500 dark:text-gray-400 ml-2">
                                Created: {formatTimestamp(label.createdAt)}
                              </span>
                            </div>
                            <button
                              onClick={() => setSelectedLabel(label)}
                              className="px-3 py-1 text-sm bg-blue-100 text-blue-800 rounded-md hover:bg-blue-200"
                            >
                              View
                            </button>
                          </div>
                        ))}
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        )}

        {/* Create User Modal */}
        {showCreateUserModal && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-lg max-h-[90vh] overflow-y-auto">
              <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-white">
                Create User
              </h2>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Username
                  </label>
                  <input
                    type="text"
                    value={userForm.name}
                    onChange={(e) => setUserForm({ ...userForm, name: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Password
                  </label>
                  <input
                    type="password"
                    value={userForm.password}
                    onChange={(e) => setUserForm({ ...userForm, password: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Roles
                  </label>
                  <div className="space-y-2">
                    {roles.map((role) => (
                      <label key={role.name} className="flex items-center">
                        <input
                          type="checkbox"
                          checked={userForm.roles.includes(role.name)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setUserForm({
                                ...userForm,
                                roles: [...userForm.roles, role.name],
                              });
                            } else {
                              setUserForm({
                                ...userForm,
                                roles: userForm.roles.filter(r => r !== role.name),
                              });
                            }
                          }}
                          className="mr-2"
                        />
                        <span className="text-sm text-gray-900 dark:text-white">{role.name}</span>
                      </label>
                    ))}
                  </div>
                </div>
                {userFormError && (
                  <div className="text-sm text-red-600 dark:text-red-400">
                    {userFormError}
                  </div>
                )}
                <div className="flex justify-end space-x-3">
                  <button
                    onClick={() => {
                      setShowCreateUserModal(false);
                      setUserForm({ name: '', password: '', roles: [] });
                      setUserFormError(null);
                    }}
                    className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleCreateUser}
                    className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700"
                  >
                    Create
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Edit User Modal */}
        {showUserDetailsModal && selectedUser && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-lg max-h-[90vh] overflow-y-auto">
              <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-white">
                Edit User: {selectedUser.name}
              </h2>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    New Password (leave empty to keep current)
                  </label>
                  <input
                    type="password"
                    value={userForm.password}
                    onChange={(e) => setUserForm({ ...userForm, password: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Roles
                  </label>
                  <div className="space-y-2">
                    {roles.map((role) => (
                      <label key={role.name} className="flex items-center">
                        <input
                          type="checkbox"
                          checked={userForm.roles.includes(role.name)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setUserForm({
                                ...userForm,
                                roles: [...userForm.roles, role.name],
                              });
                            } else {
                              setUserForm({
                                ...userForm,
                                roles: userForm.roles.filter(r => r !== role.name),
                              });
                            }
                          }}
                          className="mr-2"
                        />
                        <span className="text-sm text-gray-900 dark:text-white">{role.name}</span>
                      </label>
                    ))}
                  </div>
                </div>
                {userFormError && (
                  <div className="text-sm text-red-600 dark:text-red-400">
                    {userFormError}
                  </div>
                )}
                <div className="flex justify-end space-x-3">
                  <button
                    onClick={() => {
                      setShowUserDetailsModal(false);
                      setSelectedUser(null);
                      setUserForm({ name: '', password: '', roles: [] });
                      setUserFormError(null);
                    }}
                    className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleUpdateUser}
                    className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700"
                  >
                    Update
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </DashboardLayout>
    </ProtectedRoute>
  );
}
