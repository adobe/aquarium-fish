import React, { useState, useEffect } from 'react';
import { DashboardLayout } from '../components/DashboardLayout';
import { useAuth } from '../contexts/AuthContext';

export function meta() {
  return [
    { title: 'Status - Aquarium Fish' },
    { name: 'description', content: 'System status and node information' },
  ];
}

interface Node {
  id: string;
  name: string;
  status: 'online' | 'offline' | 'maintenance';
  location: string;
  lastSeen: string;
  resources: {
    cpu: {
      usage: number;
      total: number;
    };
    memory: {
      usage: number;
      total: number;
    };
    storage: {
      usage: number;
      total: number;
    };
  };
  applications: number;
}

export default function Status() {
  const { user, hasPermission } = useAuth();
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Mock data for now - will be replaced with real ConnectRPC streaming
  useEffect(() => {
    const mockNodes: Node[] = [
      {
        id: '1',
        name: 'node-1',
        status: 'online',
        location: 'us-west-2',
        lastSeen: '2025-01-12T12:00:00Z',
        resources: {
          cpu: { usage: 2.5, total: 8 },
          memory: { usage: 12, total: 32 },
          storage: { usage: 150, total: 500 },
        },
        applications: 5,
      },
      {
        id: '2',
        name: 'node-2',
        status: 'online',
        location: 'us-east-1',
        lastSeen: '2025-01-12T11:58:00Z',
        resources: {
          cpu: { usage: 1.2, total: 4 },
          memory: { usage: 8, total: 16 },
          storage: { usage: 80, total: 200 },
        },
        applications: 3,
      },
      {
        id: '3',
        name: 'node-3',
        status: 'maintenance',
        location: 'eu-west-1',
        lastSeen: '2025-01-12T10:30:00Z',
        resources: {
          cpu: { usage: 0, total: 16 },
          memory: { usage: 0, total: 64 },
          storage: { usage: 200, total: 1000 },
        },
        applications: 0,
      },
    ];

    // Simulate API call
    setTimeout(() => {
      setNodes(mockNodes);
      setLoading(false);
    }, 1000);
  }, []);

  const getStatusColor = (status: Node['status']) => {
    switch (status) {
      case 'online':
        return 'bg-green-100 text-green-800 dark:bg-green-900/20 dark:text-green-400';
      case 'offline':
        return 'bg-red-100 text-red-800 dark:bg-red-900/20 dark:text-red-400';
      case 'maintenance':
        return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/20 dark:text-yellow-400';
      default:
        return 'bg-gray-100 text-gray-800 dark:bg-gray-900/20 dark:text-gray-400';
    }
  };

  const getStatusIcon = (status: Node['status']) => {
    switch (status) {
      case 'online':
        return '🟢';
      case 'offline':
        return '🔴';
      case 'maintenance':
        return '🟡';
      default:
        return '⚪';
    }
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString() + ' ' + new Date(dateString).toLocaleTimeString();
  };

  const getUsageColor = (usage: number, total: number) => {
    const percentage = (usage / total) * 100;
    if (percentage < 50) return 'bg-green-500';
    if (percentage < 80) return 'bg-yellow-500';
    return 'bg-red-500';
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 GB';
    const k = 1024;
    const sizes = ['GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  if (loading) {
    return (
      <DashboardLayout>
        <div className="flex items-center justify-center h-64">
          <div className="text-center">
            <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto mb-4"></div>
            <p className="text-gray-600 dark:text-gray-300">Loading system status...</p>
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
              Error loading system status: {error}
            </div>
          </div>
        </div>
      </DashboardLayout>
    );
  }

  const totalNodes = nodes.length;
  const onlineNodes = nodes.filter(n => n.status === 'online').length;
  const totalApplications = nodes.reduce((sum, node) => sum + node.applications, 0);

  return (
    <DashboardLayout>
      <div className="space-y-6">
        {/* Header */}
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">System Status</h1>
          <p className="text-gray-600 dark:text-gray-400">
            Monitor nodes and system resources
          </p>
        </div>

        {/* Real-time status indicator */}
        <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md p-4">
          <div className="flex items-center">
            <div className="animate-pulse w-3 h-3 bg-green-500 rounded-full mr-3"></div>
            <div className="text-sm text-green-700 dark:text-green-300">
              Real-time monitoring active (ConnectRPC streaming)
            </div>
          </div>
        </div>

        {/* System overview */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700 p-6">
            <div className="flex items-center">
              <div className="text-2xl mr-3">🖥️</div>
              <div>
                <div className="text-2xl font-bold text-gray-900 dark:text-white">{onlineNodes}/{totalNodes}</div>
                <div className="text-sm text-gray-600 dark:text-gray-400">Nodes Online</div>
              </div>
            </div>
          </div>

          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700 p-6">
            <div className="flex items-center">
              <div className="text-2xl mr-3">📱</div>
              <div>
                <div className="text-2xl font-bold text-gray-900 dark:text-white">{totalApplications}</div>
                <div className="text-sm text-gray-600 dark:text-gray-400">Total Applications</div>
              </div>
            </div>
          </div>

          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700 p-6">
            <div className="flex items-center">
              <div className="text-2xl mr-3">⚡</div>
              <div>
                <div className="text-2xl font-bold text-green-600 dark:text-green-400">Healthy</div>
                <div className="text-sm text-gray-600 dark:text-gray-400">System Status</div>
              </div>
            </div>
          </div>
        </div>

        {/* Nodes list */}
        <div className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Nodes</h2>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {nodes.map((node) => (
              <div key={node.id} className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700 p-6">
                <div className="flex items-center justify-between mb-4">
                  <div className="flex items-center">
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mr-3">{node.name}</h3>
                    <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getStatusColor(node.status)}`}>
                      <span className="mr-1">{getStatusIcon(node.status)}</span>
                      {node.status}
                    </span>
                  </div>
                  <div className="text-sm text-gray-600 dark:text-gray-400">
                    {node.applications} apps
                  </div>
                </div>

                <div className="space-y-3 mb-4">
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-600 dark:text-gray-400">Location:</span>
                    <span className="text-gray-900 dark:text-white">{node.location}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-600 dark:text-gray-400">Last Seen:</span>
                    <span className="text-gray-900 dark:text-white">{formatDate(node.lastSeen)}</span>
                  </div>
                </div>

                {/* Resource usage */}
                <div className="space-y-3">
                  <div>
                    <div className="flex justify-between text-sm mb-1">
                      <span className="text-gray-600 dark:text-gray-400">CPU</span>
                      <span className="text-gray-900 dark:text-white">{node.resources.cpu.usage}/{node.resources.cpu.total} cores</span>
                    </div>
                    <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                      <div
                        className={`h-2 rounded-full ${getUsageColor(node.resources.cpu.usage, node.resources.cpu.total)}`}
                        style={{ width: `${(node.resources.cpu.usage / node.resources.cpu.total) * 100}%` }}
                      ></div>
                    </div>
                  </div>

                  <div>
                    <div className="flex justify-between text-sm mb-1">
                      <span className="text-gray-600 dark:text-gray-400">Memory</span>
                      <span className="text-gray-900 dark:text-white">{node.resources.memory.usage}/{node.resources.memory.total} GB</span>
                    </div>
                    <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                      <div
                        className={`h-2 rounded-full ${getUsageColor(node.resources.memory.usage, node.resources.memory.total)}`}
                        style={{ width: `${(node.resources.memory.usage / node.resources.memory.total) * 100}%` }}
                      ></div>
                    </div>
                  </div>

                  <div>
                    <div className="flex justify-between text-sm mb-1">
                      <span className="text-gray-600 dark:text-gray-400">Storage</span>
                      <span className="text-gray-900 dark:text-white">{formatBytes(node.resources.storage.usage)}/{formatBytes(node.resources.storage.total)}</span>
                    </div>
                    <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                      <div
                        className={`h-2 rounded-full ${getUsageColor(node.resources.storage.usage, node.resources.storage.total)}`}
                        style={{ width: `${(node.resources.storage.usage / node.resources.storage.total) * 100}%` }}
                      ></div>
                    </div>
                  </div>
                </div>

                {hasPermission('node', 'read') && (
                  <div className="mt-4">
                    <button className="w-full bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 hover:bg-blue-100 dark:hover:bg-blue-900/30 px-3 py-2 rounded-md text-sm font-medium">
                      View Details
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
    </DashboardLayout>
  );
}
