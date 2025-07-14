import React, { useState, useEffect } from 'react';
import { DashboardLayout } from '../components/DashboardLayout';
import { ProtectedRoute } from '../components/ProtectedRoute';
import { useAuth } from '../contexts/AuthContext';

export function meta() {
  return [
    { title: 'Applications - Aquarium Fish' },
    { name: 'description', content: 'Manage and monitor applications' },
  ];
}

interface Application {
  id: string;
  name: string;
  status: 'running' | 'stopped' | 'pending' | 'error';
  createdAt: string;
  resources: {
    cpu: string;
    memory: string;
    storage: string;
  };
}

export default function Applications() {
  const { user, hasPermission } = useAuth();
  const [applications, setApplications] = useState<Application[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Mock data for now - will be replaced with real ConnectRPC streaming
  useEffect(() => {
    const mockApps: Application[] = [
      {
        id: '1',
        name: 'web-frontend',
        status: 'running',
        createdAt: '2024-01-15T10:30:00Z',
        resources: {
          cpu: '0.5 vCPU',
          memory: '1 GB',
          storage: '10 GB',
        },
      },
      {
        id: '2',
        name: 'api-backend',
        status: 'running',
        createdAt: '2024-01-15T10:25:00Z',
        resources: {
          cpu: '1 vCPU',
          memory: '2 GB',
          storage: '20 GB',
        },
      },
      {
        id: '3',
        name: 'database',
        status: 'running',
        createdAt: '2024-01-15T10:20:00Z',
        resources: {
          cpu: '2 vCPU',
          memory: '4 GB',
          storage: '100 GB',
        },
      },
      {
        id: '4',
        name: 'cache-service',
        status: 'pending',
        createdAt: '2024-01-15T11:00:00Z',
        resources: {
          cpu: '0.25 vCPU',
          memory: '512 MB',
          storage: '5 GB',
        },
      },
    ];

    // Simulate loading
    setTimeout(() => {
      setApplications(mockApps);
      setLoading(false);
    }, 1000);
  }, []);

  const getStatusColor = (status: Application['status']) => {
    switch (status) {
      case 'running':
        return 'bg-green-100 text-green-800 dark:bg-green-900/20 dark:text-green-400';
      case 'stopped':
        return 'bg-red-100 text-red-800 dark:bg-red-900/20 dark:text-red-400';
      case 'pending':
        return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/20 dark:text-yellow-400';
      case 'error':
        return 'bg-red-100 text-red-800 dark:bg-red-900/20 dark:text-red-400';
      default:
        return 'bg-gray-100 text-gray-800 dark:bg-gray-900/20 dark:text-gray-400';
    }
  };

  const getStatusIcon = (status: Application['status']) => {
    switch (status) {
      case 'running':
        return '🟢';
      case 'stopped':
        return '🔴';
      case 'pending':
        return '🟡';
      case 'error':
        return '🔴';
      default:
        return '⚪';
    }
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

  return (
    <ProtectedRoute>
      <DashboardLayout>
        <div className="space-y-6">
          {/* Header */}
          <div className="flex justify-between items-center">
            <div>
              <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Applications</h1>
              <p className="text-gray-600 dark:text-gray-300">
                Manage and monitor your applications
              </p>
            </div>
            {hasPermission('applications', 'create') && (
              <button className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md font-medium">
                Create Application
              </button>
            )}
          </div>

          {/* Stats Cards */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
              <div className="flex items-center">
                <div className="flex-shrink-0">
                  <div className="w-8 h-8 bg-green-500 rounded-full flex items-center justify-center">
                    <span className="text-white font-bold">R</span>
                  </div>
                </div>
                <div className="ml-5 w-0 flex-1">
                  <dl>
                    <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                      Running
                    </dt>
                    <dd className="text-lg font-medium text-gray-900 dark:text-white">
                      {applications.filter(app => app.status === 'running').length}
                    </dd>
                  </dl>
                </div>
              </div>
            </div>

            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
              <div className="flex items-center">
                <div className="flex-shrink-0">
                  <div className="w-8 h-8 bg-yellow-500 rounded-full flex items-center justify-center">
                    <span className="text-white font-bold">P</span>
                  </div>
                </div>
                <div className="ml-5 w-0 flex-1">
                  <dl>
                    <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                      Pending
                    </dt>
                    <dd className="text-lg font-medium text-gray-900 dark:text-white">
                      {applications.filter(app => app.status === 'pending').length}
                    </dd>
                  </dl>
                </div>
              </div>
            </div>

            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
              <div className="flex items-center">
                <div className="flex-shrink-0">
                  <div className="w-8 h-8 bg-red-500 rounded-full flex items-center justify-center">
                    <span className="text-white font-bold">S</span>
                  </div>
                </div>
                <div className="ml-5 w-0 flex-1">
                  <dl>
                    <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                      Stopped
                    </dt>
                    <dd className="text-lg font-medium text-gray-900 dark:text-white">
                      {applications.filter(app => app.status === 'stopped').length}
                    </dd>
                  </dl>
                </div>
              </div>
            </div>

            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
              <div className="flex items-center">
                <div className="flex-shrink-0">
                  <div className="w-8 h-8 bg-blue-500 rounded-full flex items-center justify-center">
                    <span className="text-white font-bold">T</span>
                  </div>
                </div>
                <div className="ml-5 w-0 flex-1">
                  <dl>
                    <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                      Total
                    </dt>
                    <dd className="text-lg font-medium text-gray-900 dark:text-white">
                      {applications.length}
                    </dd>
                  </dl>
                </div>
              </div>
            </div>
          </div>

          {/* Applications List */}
          <div className="bg-white dark:bg-gray-800 shadow rounded-lg">
            <div className="px-4 py-5 sm:p-6">
              <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-4">
                Application List
              </h3>

              {loading ? (
                <div className="text-center py-8">
                  <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto mb-4"></div>
                  <p className="text-gray-600 dark:text-gray-300">Loading applications...</p>
                </div>
              ) : error ? (
                <div className="text-center py-8">
                  <p className="text-red-600 dark:text-red-400">{error}</p>
                </div>
              ) : (
                <div className="space-y-4">
                  {applications.map((app) => (
                    <div key={app.id} className="border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                      <div className="flex items-center justify-between">
                        <div className="flex items-center space-x-3">
                          <span className="text-lg">{getStatusIcon(app.status)}</span>
                          <div>
                            <h4 className="text-lg font-medium text-gray-900 dark:text-white">
                              {app.name}
                            </h4>
                            <p className="text-sm text-gray-500 dark:text-gray-400">
                              Created: {formatDate(app.createdAt)}
                            </p>
                          </div>
                        </div>
                        <div className="flex items-center space-x-4">
                          <span className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getStatusColor(app.status)}`}>
                            {app.status}
                          </span>
                          {hasPermission('applications', 'update') && (
                            <button className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300">
                              Configure
                            </button>
                          )}
                        </div>
                      </div>

                      <div className="mt-4 grid grid-cols-3 gap-4 text-sm">
                        <div>
                          <span className="font-medium text-gray-900 dark:text-white">CPU:</span>
                          <span className="ml-2 text-gray-600 dark:text-gray-300">{app.resources.cpu}</span>
                        </div>
                        <div>
                          <span className="font-medium text-gray-900 dark:text-white">Memory:</span>
                          <span className="ml-2 text-gray-600 dark:text-gray-300">{app.resources.memory}</span>
                        </div>
                        <div>
                          <span className="font-medium text-gray-900 dark:text-white">Storage:</span>
                          <span className="ml-2 text-gray-600 dark:text-gray-300">{app.resources.storage}</span>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      </DashboardLayout>
    </ProtectedRoute>
  );
}
