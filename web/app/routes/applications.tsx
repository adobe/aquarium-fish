import React, { useState, useEffect } from 'react';
import { DashboardLayout } from '../components/DashboardLayout';
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
    const mockApplications: Application[] = [
      {
        id: '1',
        name: 'web-app-prod',
        status: 'running',
        createdAt: '2025-01-12T10:00:00Z',
        resources: {
          cpu: '0.5 cores',
          memory: '512 MB',
          storage: '1 GB',
        },
      },
      {
        id: '2',
        name: 'api-service',
        status: 'running',
        createdAt: '2025-01-12T09:30:00Z',
        resources: {
          cpu: '1 core',
          memory: '1 GB',
          storage: '2 GB',
        },
      },
      {
        id: '3',
        name: 'batch-processor',
        status: 'stopped',
        createdAt: '2025-01-12T08:00:00Z',
        resources: {
          cpu: '2 cores',
          memory: '2 GB',
          storage: '10 GB',
        },
      },
    ];

    // Simulate API call
    setTimeout(() => {
      setApplications(mockApplications);
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
        return '❌';
      default:
        return '⚪';
    }
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString() + ' ' + new Date(dateString).toLocaleTimeString();
  };

  if (loading) {
    return (
      <DashboardLayout>
        <div className="flex items-center justify-center h-64">
          <div className="text-center">
            <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto mb-4"></div>
            <p className="text-gray-600 dark:text-gray-300">Loading applications...</p>
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
              Error loading applications: {error}
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
            <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Applications</h1>
            <p className="text-gray-600 dark:text-gray-400">
              Manage and monitor your applications
            </p>
          </div>
          {hasPermission('application', 'create') && (
            <button className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md text-sm font-medium">
              Create Application
            </button>
          )}
        </div>

        {/* Real-time status indicator */}
        <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md p-4">
          <div className="flex items-center">
            <div className="animate-pulse w-3 h-3 bg-green-500 rounded-full mr-3"></div>
            <div className="text-sm text-green-700 dark:text-green-300">
              Real-time updates active (ConnectRPC streaming)
            </div>
          </div>
        </div>

        {/* Applications grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {applications.map((app) => (
            <div key={app.id} className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700 p-6">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-medium text-gray-900 dark:text-white">{app.name}</h3>
                <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getStatusColor(app.status)}`}>
                  <span className="mr-1">{getStatusIcon(app.status)}</span>
                  {app.status}
                </span>
              </div>

              <div className="space-y-3">
                <div className="text-sm text-gray-600 dark:text-gray-400">
                  <div className="font-medium">Created:</div>
                  <div>{formatDate(app.createdAt)}</div>
                </div>

                <div className="text-sm text-gray-600 dark:text-gray-400">
                  <div className="font-medium mb-1">Resources:</div>
                  <div className="space-y-1">
                    <div>CPU: {app.resources.cpu}</div>
                    <div>Memory: {app.resources.memory}</div>
                    <div>Storage: {app.resources.storage}</div>
                  </div>
                </div>
              </div>

              <div className="mt-4 flex space-x-2">
                {hasPermission('application', 'read') && (
                  <button className="flex-1 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 hover:bg-blue-100 dark:hover:bg-blue-900/30 px-3 py-2 rounded-md text-sm font-medium">
                    View Details
                  </button>
                )}
                {hasPermission('application', 'update') && (
                  <button className="flex-1 bg-gray-50 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600 px-3 py-2 rounded-md text-sm font-medium">
                    {app.status === 'running' ? 'Stop' : 'Start'}
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>

        {applications.length === 0 && (
          <div className="text-center py-12">
            <div className="text-gray-500 dark:text-gray-400">
              <div className="text-6xl mb-4">📱</div>
              <h3 className="text-lg font-medium mb-2">No applications found</h3>
              <p className="text-sm">Create your first application to get started.</p>
            </div>
          </div>
        )}
      </div>
    </DashboardLayout>
  );
}
