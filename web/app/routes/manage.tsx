import React, { useState, useEffect } from 'react';
import { DashboardLayout } from '../components/DashboardLayout';
import { ProtectedRoute } from '../components/ProtectedRoute';
import { useAuth } from '../contexts/AuthContext';

export function meta() {
  return [
    { title: 'Manage - Aquarium Fish' },
    { name: 'description', content: 'Manage labels and users' },
  ];
}

export default function Manage() {
  const { user, hasPermission } = useAuth();
  const [activeTab, setActiveTab] = useState<'labels' | 'users'>('labels');
  const [yamlContent, setYamlContent] = useState('');
  const [isYamlModalOpen, setIsYamlModalOpen] = useState(false);
  const [yamlModalTitle, setYamlModalTitle] = useState('');
  const [yamlModalType, setYamlModalType] = useState<'create' | 'edit'>('create');

  // Mock data for labels
  const [labels, setLabels] = useState([
    {
      id: '1',
      name: 'production',
      type: 'environment',
      resources: {
        cpu: '8 cores',
        memory: '32 GB',
        storage: '500 GB'
      },
      nodeCount: 3,
      status: 'active',
      createdAt: '2024-01-15T10:00:00Z',
    },
    {
      id: '2',
      name: 'staging',
      type: 'environment',
      resources: {
        cpu: '4 cores',
        memory: '16 GB',
        storage: '200 GB'
      },
      nodeCount: 2,
      status: 'active',
      createdAt: '2024-01-15T09:30:00Z',
    },
  ]);

  // Mock data for users
  const [users, setUsers] = useState([
    {
      id: '1',
      username: 'admin',
      roles: ['admin', 'user'],
      permissions: ['*'],
      createdAt: '2024-01-01T00:00:00Z',
      lastActive: '2024-01-15T12:00:00Z',
      status: 'active',
    },
    {
      id: '2',
      username: 'developer',
      roles: ['developer'],
      permissions: ['applications.read', 'applications.create'],
      createdAt: '2024-01-10T10:00:00Z',
      lastActive: '2024-01-15T11:30:00Z',
      status: 'active',
    },
  ]);

  const handleYamlSubmit = () => {
    // TODO: Implement YAML submission logic
    console.log('YAML Content:', yamlContent);
    setIsYamlModalOpen(false);
    setYamlContent('');
  };

  const openYamlModal = (title: string, type: 'create' | 'edit', content: string = '') => {
    setYamlModalTitle(title);
    setYamlModalType(type);
    setYamlContent(content);
    setIsYamlModalOpen(true);
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-100 text-green-800 dark:bg-green-900/20 dark:text-green-400';
      case 'inactive':
        return 'bg-gray-100 text-gray-800 dark:bg-gray-900/20 dark:text-gray-400';
      case 'suspended':
        return 'bg-red-100 text-red-800 dark:bg-red-900/20 dark:text-red-400';
      default:
        return 'bg-gray-100 text-gray-800 dark:bg-gray-900/20 dark:text-gray-400';
    }
  };

  return (
    <ProtectedRoute>
      <DashboardLayout>
        <div className="space-y-6">
          {/* Header */}
          <div>
            <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Management</h1>
            <p className="text-gray-600 dark:text-gray-300">
              Manage labels and users using YAML configuration
            </p>
          </div>

          {/* Tabs */}
          <div className="border-b border-gray-200 dark:border-gray-700">
            <nav className="flex space-x-8">
              <button
                onClick={() => setActiveTab('labels')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  activeTab === 'labels'
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                }`}
              >
                Labels
              </button>
              <button
                onClick={() => setActiveTab('users')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  activeTab === 'users'
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                }`}
              >
                Users
              </button>
            </nav>
          </div>

          {/* Labels Tab */}
          {activeTab === 'labels' && (
            <div className="space-y-6">
              <div className="flex justify-between items-center">
                <h2 className="text-lg font-medium text-gray-900 dark:text-white">Labels</h2>
                {hasPermission('labels', 'create') && (
                  <button
                    onClick={() => openYamlModal('Create Label', 'create',
                      'apiVersion: v1\nkind: Label\nmetadata:\n  name: my-label\nspec:\n  type: environment\n  resources:\n    cpu: "4 cores"\n    memory: "16 GB"\n    storage: "100 GB"'
                    )}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md font-medium"
                  >
                    Create Label
                  </button>
                )}
              </div>

              <div className="bg-white dark:bg-gray-800 shadow rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <div className="space-y-4">
                    {labels.map((label) => (
                      <div key={label.id} className="border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                        <div className="flex items-center justify-between mb-4">
                          <div className="flex items-center space-x-3">
                            <div className="w-8 h-8 bg-blue-500 rounded-full flex items-center justify-center">
                              <span className="text-white font-bold text-sm">L</span>
                            </div>
                            <div>
                              <h4 className="text-lg font-medium text-gray-900 dark:text-white">
                                {label.name}
                              </h4>
                              <p className="text-sm text-gray-500 dark:text-gray-400">
                                Type: {label.type} • {label.nodeCount} nodes
                              </p>
                            </div>
                          </div>
                          <div className="flex items-center space-x-4">
                            <span className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getStatusColor(label.status)}`}>
                              {label.status}
                            </span>
                            {hasPermission('labels', 'update') && (
                              <button
                                onClick={() => openYamlModal('Edit Label', 'edit',
                                  `apiVersion: v1\nkind: Label\nmetadata:\n  name: ${label.name}\nspec:\n  type: ${label.type}\n  resources:\n    cpu: "${label.resources.cpu}"\n    memory: "${label.resources.memory}"\n    storage: "${label.resources.storage}"`
                                )}
                                className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                              >
                                Edit
                              </button>
                            )}
                          </div>
                        </div>

                        <div className="grid grid-cols-3 gap-4 text-sm">
                          <div>
                            <span className="font-medium text-gray-900 dark:text-white">CPU:</span>
                            <span className="ml-2 text-gray-600 dark:text-gray-300">{label.resources.cpu}</span>
                          </div>
                          <div>
                            <span className="font-medium text-gray-900 dark:text-white">Memory:</span>
                            <span className="ml-2 text-gray-600 dark:text-gray-300">{label.resources.memory}</span>
                          </div>
                          <div>
                            <span className="font-medium text-gray-900 dark:text-white">Storage:</span>
                            <span className="ml-2 text-gray-600 dark:text-gray-300">{label.resources.storage}</span>
                          </div>
                        </div>

                        <div className="mt-2 text-sm text-gray-500 dark:text-gray-400">
                          Created: {formatDate(label.createdAt)}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Users Tab */}
          {activeTab === 'users' && (
            <div className="space-y-6">
              <div className="flex justify-between items-center">
                <h2 className="text-lg font-medium text-gray-900 dark:text-white">Users</h2>
                {hasPermission('users', 'create') && (
                  <button
                    onClick={() => openYamlModal('Create User', 'create',
                      'apiVersion: v1\nkind: User\nmetadata:\n  name: username\nspec:\n  roles:\n    - user\n  permissions:\n    - applications.read\n    - applications.create'
                    )}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md font-medium"
                  >
                    Create User
                  </button>
                )}
              </div>

              <div className="bg-white dark:bg-gray-800 shadow rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <div className="space-y-4">
                    {users.map((user) => (
                      <div key={user.id} className="border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                        <div className="flex items-center justify-between mb-4">
                          <div className="flex items-center space-x-3">
                            <div className="w-8 h-8 bg-purple-500 rounded-full flex items-center justify-center">
                              <span className="text-white font-bold text-sm">
                                {user.username.charAt(0).toUpperCase()}
                              </span>
                            </div>
                            <div>
                              <h4 className="text-lg font-medium text-gray-900 dark:text-white">
                                {user.username}
                              </h4>
                              <p className="text-sm text-gray-500 dark:text-gray-400">
                                Roles: {user.roles.join(', ')}
                              </p>
                            </div>
                          </div>
                          <div className="flex items-center space-x-4">
                            <span className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getStatusColor(user.status)}`}>
                              {user.status}
                            </span>
                            {hasPermission('users', 'update') && (
                              <button
                                onClick={() => openYamlModal('Edit User', 'edit',
                                  `apiVersion: v1\nkind: User\nmetadata:\n  name: ${user.username}\nspec:\n  roles:\n${user.roles.map(r => `    - ${r}`).join('\n')}\n  permissions:\n${user.permissions.map(p => `    - ${p}`).join('\n')}`
                                )}
                                className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                              >
                                Edit
                              </button>
                            )}
                          </div>
                        </div>

                        <div className="space-y-2 text-sm">
                          <div>
                            <span className="font-medium text-gray-900 dark:text-white">Permissions:</span>
                            <div className="mt-1 flex flex-wrap gap-2">
                              {user.permissions.map((permission, index) => (
                                <span key={index} className="inline-flex px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded">
                                  {permission}
                                </span>
                              ))}
                            </div>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-gray-500 dark:text-gray-400">
                              Created: {formatDate(user.createdAt)}
                            </span>
                            <span className="text-gray-500 dark:text-gray-400">
                              Last Active: {formatDate(user.lastActive)}
                            </span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* YAML Modal */}
        {isYamlModalOpen && (
          <div className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full z-50">
            <div className="relative top-20 mx-auto p-5 border w-11/12 max-w-4xl shadow-lg rounded-md bg-white dark:bg-gray-800">
              <div className="mt-3">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white">
                    {yamlModalTitle}
                  </h3>
                  <button
                    onClick={() => setIsYamlModalOpen(false)}
                    className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
                  >
                    <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>

                <div className="mb-4">
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    YAML Configuration
                  </label>
                  <textarea
                    value={yamlContent}
                    onChange={(e) => setYamlContent(e.target.value)}
                    rows={20}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm focus:outline-none focus:ring-blue-500 focus:border-blue-500 dark:bg-gray-700 dark:text-white font-mono text-sm"
                    placeholder="Enter YAML configuration..."
                  />
                </div>

                <div className="flex justify-end space-x-3">
                  <button
                    onClick={() => setIsYamlModalOpen(false)}
                    className="px-4 py-2 text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-md hover:bg-gray-200 dark:hover:bg-gray-600"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleYamlSubmit}
                    className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700"
                  >
                    {yamlModalType === 'create' ? 'Create' : 'Update'}
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
