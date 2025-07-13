import React, { useState, useEffect } from 'react';
import { DashboardLayout } from '../components/DashboardLayout';
import { useAuth } from '../contexts/AuthContext';

export function meta() {
  return [
    { title: 'Manage - Aquarium Fish' },
    { name: 'description', content: 'Manage labels and users' },
  ];
}

interface Label {
  id: string;
  name: string;
  description: string;
  resources: {
    cpu: string;
    memory: string;
    storage: string;
  };
  createdAt: string;
  applications: number;
}

interface User {
  id: string;
  username: string;
  email: string;
  roles: string[];
  lastLogin: string;
  status: 'active' | 'inactive';
}

export default function Manage() {
  const { user, hasPermission } = useAuth();
  const [activeTab, setActiveTab] = useState<'labels' | 'users'>('labels');
  const [labels, setLabels] = useState<Label[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [yamlInput, setYamlInput] = useState('');
  const [showYamlEditor, setShowYamlEditor] = useState(false);

  // Mock data for now - will be replaced with real ConnectRPC calls
  useEffect(() => {
    const mockLabels: Label[] = [
      {
        id: '1',
        name: 'ubuntu-20.04',
        description: 'Ubuntu 20.04 LTS with development tools',
        resources: {
          cpu: '2 cores',
          memory: '4 GB',
          storage: '20 GB',
        },
        createdAt: '2025-01-10T10:00:00Z',
        applications: 12,
      },
      {
        id: '2',
        name: 'nodejs-18',
        description: 'Node.js 18 runtime environment',
        resources: {
          cpu: '1 core',
          memory: '2 GB',
          storage: '10 GB',
        },
        createdAt: '2025-01-11T15:30:00Z',
        applications: 8,
      },
    ];

    const mockUsers: User[] = [
      {
        id: '1',
        username: 'admin',
        email: 'admin@company.com',
        roles: ['admin', 'user'],
        lastLogin: '2025-01-12T11:00:00Z',
        status: 'active',
      },
      {
        id: '2',
        username: 'developer',
        email: 'dev@company.com',
        roles: ['user'],
        lastLogin: '2025-01-11T16:45:00Z',
        status: 'active',
      },
    ];

    // Simulate API call
    setTimeout(() => {
      setLabels(mockLabels);
      setUsers(mockUsers);
      setLoading(false);
    }, 1000);
  }, []);

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString() + ' ' + new Date(dateString).toLocaleTimeString();
  };

  const handleYamlSubmit = () => {
    // TODO: Implement YAML submission via ConnectRPC
    console.log('YAML submitted:', yamlInput);
    setShowYamlEditor(false);
    setYamlInput('');
  };

  const getSampleYaml = () => {
    if (activeTab === 'labels') {
      return `# Label configuration
name: my-new-label
description: A custom label for development
resources:
  cpu: "2 cores"
  memory: "4 GB"
  storage: "20 GB"
image:
  name: ubuntu
  tag: "20.04"
environment:
  - NODE_ENV=development
  - PORT=3000`;
    } else {
      return `# User configuration
username: newuser
email: user@company.com
roles:
  - user
permissions:
  - resource: application
    action: read
  - resource: label
    action: read`;
    }
  };

  if (loading) {
    return (
      <DashboardLayout>
        <div className="flex items-center justify-center h-64">
          <div className="text-center">
            <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto mb-4"></div>
            <p className="text-gray-600 dark:text-gray-300">Loading...</p>
          </div>
        </div>
      </DashboardLayout>
    );
  }

  if (error) {
    return (
      <DashboardLayout>
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-4">
          <div className="flex">
            <div className="text-sm text-red-700 dark:text-red-300">
              Error loading data: {error}
            </div>
          </div>
        </div>
      </DashboardLayout>
    );
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Manage</h1>
            <p className="text-gray-600 dark:text-gray-400">
              Manage labels and users with YAML configuration
            </p>
          </div>
          {hasPermission(activeTab === 'labels' ? 'label' : 'user', 'create') && (
            <button
              onClick={() => setShowYamlEditor(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md text-sm font-medium"
            >
              Create {activeTab === 'labels' ? 'Label' : 'User'}
            </button>
          )}
        </div>

        {/* Tabs */}
        <div className="border-b border-gray-200 dark:border-gray-700">
          <nav className="-mb-px flex space-x-8">
            <button
              onClick={() => setActiveTab('labels')}
              className={`py-2 px-1 border-b-2 font-medium text-sm ${
                activeTab === 'labels'
                  ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
              }`}
            >
              Labels ({labels.length})
            </button>
            <button
              onClick={() => setActiveTab('users')}
              className={`py-2 px-1 border-b-2 font-medium text-sm ${
                activeTab === 'users'
                  ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
              }`}
            >
              Users ({users.length})
            </button>
          </nav>
        </div>

        {/* Content */}
        {activeTab === 'labels' && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {labels.map((label) => (
              <div key={label.id} className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700 p-6">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white">{label.name}</h3>
                  <span className="text-sm text-gray-500 dark:text-gray-400">
                    {label.applications} apps
                  </span>
                </div>

                <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
                  {label.description}
                </p>

                <div className="space-y-2 mb-4">
                  <div className="text-sm text-gray-600 dark:text-gray-400">
                    <span className="font-medium">Resources:</span>
                  </div>
                  <div className="text-sm text-gray-900 dark:text-white space-y-1">
                    <div>CPU: {label.resources.cpu}</div>
                    <div>Memory: {label.resources.memory}</div>
                    <div>Storage: {label.resources.storage}</div>
                  </div>
                </div>

                <div className="text-sm text-gray-600 dark:text-gray-400 mb-4">
                  Created: {formatDate(label.createdAt)}
                </div>

                <div className="flex space-x-2">
                  {hasPermission('label', 'read') && (
                    <button className="flex-1 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 hover:bg-blue-100 dark:hover:bg-blue-900/30 px-3 py-2 rounded-md text-sm font-medium">
                      View
                    </button>
                  )}
                  {hasPermission('label', 'update') && (
                    <button className="flex-1 bg-gray-50 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600 px-3 py-2 rounded-md text-sm font-medium">
                      Edit
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        {activeTab === 'users' && (
          <div className="bg-white dark:bg-gray-800 shadow-sm rounded-lg border border-gray-200 dark:border-gray-700">
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
                <thead className="bg-gray-50 dark:bg-gray-700">
                  <tr>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                      User
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                      Roles
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                      Last Login
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                      Status
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                      Actions
                    </th>
                  </tr>
                </thead>
                <tbody className="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
                  {users.map((user) => (
                    <tr key={user.id}>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center">
                          <div className="h-10 w-10 bg-blue-600 rounded-full flex items-center justify-center">
                            <span className="text-white font-medium text-sm">
                              {user.username.charAt(0).toUpperCase()}
                            </span>
                          </div>
                          <div className="ml-4">
                            <div className="text-sm font-medium text-gray-900 dark:text-white">
                              {user.username}
                            </div>
                            <div className="text-sm text-gray-500 dark:text-gray-400">
                              {user.email}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex space-x-1">
                          {user.roles.map((role) => (
                            <span
                              key={role}
                              className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/20 dark:text-blue-300"
                            >
                              {role}
                            </span>
                          ))}
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400">
                        {formatDate(user.lastLogin)}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                          user.status === 'active'
                            ? 'bg-green-100 text-green-800 dark:bg-green-900/20 dark:text-green-400'
                            : 'bg-red-100 text-red-800 dark:bg-red-900/20 dark:text-red-400'
                        }`}>
                          {user.status}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm font-medium space-x-2">
                        {hasPermission('user', 'read') && (
                          <button className="text-blue-600 hover:text-blue-900 dark:text-blue-400 dark:hover:text-blue-300">
                            View
                          </button>
                        )}
                        {hasPermission('user', 'update') && (
                          <button className="text-indigo-600 hover:text-indigo-900 dark:text-indigo-400 dark:hover:text-indigo-300">
                            Edit
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* YAML Editor Modal */}
        {showYamlEditor && (
          <div className="fixed inset-0 bg-gray-600 bg-opacity-75 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-4xl w-full mx-4 max-h-[90vh] overflow-hidden">
              <div className="flex items-center justify-between p-6 border-b border-gray-200 dark:border-gray-700">
                <h3 className="text-lg font-medium text-gray-900 dark:text-white">
                  Create {activeTab === 'labels' ? 'Label' : 'User'} with YAML
                </h3>
                <button
                  onClick={() => setShowYamlEditor(false)}
                  className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
                >
                  <span className="sr-only">Close</span>
                  <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>

              <div className="p-6 overflow-y-auto">
                <div className="mb-4">
                  <button
                    onClick={() => setYamlInput(getSampleYaml())}
                    className="text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                  >
                    Load sample YAML
                  </button>
                </div>

                <textarea
                  value={yamlInput}
                  onChange={(e) => setYamlInput(e.target.value)}
                  placeholder={`Enter YAML configuration for ${activeTab === 'labels' ? 'label' : 'user'}`}
                  className="w-full h-96 p-4 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono text-sm"
                />
              </div>

              <div className="flex justify-end space-x-3 p-6 border-t border-gray-200 dark:border-gray-700">
                <button
                  onClick={() => setShowYamlEditor(false)}
                  className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50 dark:bg-gray-700 dark:text-gray-300 dark:border-gray-600 dark:hover:bg-gray-600"
                >
                  Cancel
                </button>
                <button
                  onClick={handleYamlSubmit}
                  className="px-4 py-2 text-sm font-medium text-white bg-blue-600 border border-transparent rounded-md hover:bg-blue-700"
                >
                  Create
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </DashboardLayout>
  );
}
