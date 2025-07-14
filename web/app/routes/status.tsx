import React, { useState, useEffect } from 'react';
import { DashboardLayout } from '../components/DashboardLayout';
import { ProtectedRoute } from '../components/ProtectedRoute';
import { useAuth } from '../contexts/AuthContext';

export function meta() {
  return [
    { title: 'Status - Aquarium Fish' },
    { name: 'description', content: 'System status and monitoring' },
  ];
}

interface NodeStatus {
  id: string;
  name: string;
  status: 'healthy' | 'warning' | 'error';
  cpu: number;
  memory: number;
  disk: number;
  uptime: string;
  lastHeartbeat: string;
}

interface SystemMetrics {
  totalNodes: number;
  activeApplications: number;
  totalCPU: number;
  totalMemory: number;
  avgCPUUsage: number;
  avgMemoryUsage: number;
}

export default function Status() {
  const { user, hasPermission } = useAuth();
  const [nodes, setNodes] = useState<NodeStatus[]>([]);
  const [metrics, setMetrics] = useState<SystemMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Mock data for now - will be replaced with real ConnectRPC streaming
  useEffect(() => {
    const mockNodes: NodeStatus[] = [
      {
        id: '1',
        name: 'node-01',
        status: 'healthy',
        cpu: 45,
        memory: 62,
        disk: 35,
        uptime: '7 days, 14 hours',
        lastHeartbeat: '2025-01-12T12:00:00Z',
      },
      {
        id: '2',
        name: 'node-02',
        status: 'healthy',
        cpu: 32,
        memory: 48,
        disk: 28,
        uptime: '5 days, 8 hours',
        lastHeartbeat: '2025-01-12T12:00:00Z',
      },
      {
        id: '3',
        name: 'node-03',
        status: 'warning',
        cpu: 78,
        memory: 85,
        disk: 92,
        uptime: '2 days, 3 hours',
        lastHeartbeat: '2025-01-12T11:58:00Z',
      },
    ];

    const mockMetrics: SystemMetrics = {
      totalNodes: 3,
      activeApplications: 12,
      totalCPU: 24,
      totalMemory: 96,
      avgCPUUsage: 52,
      avgMemoryUsage: 65,
    };

    // Simulate loading
    setTimeout(() => {
      setNodes(mockNodes);
      setMetrics(mockMetrics);
      setLoading(false);
    }, 1000);
  }, []);

  const getStatusColor = (status: NodeStatus['status']) => {
    switch (status) {
      case 'healthy':
        return 'bg-green-100 text-green-800 dark:bg-green-900/20 dark:text-green-400';
      case 'warning':
        return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/20 dark:text-yellow-400';
      case 'error':
        return 'bg-red-100 text-red-800 dark:bg-red-900/20 dark:text-red-400';
      default:
        return 'bg-gray-100 text-gray-800 dark:bg-gray-900/20 dark:text-gray-400';
    }
  };

  const getStatusIcon = (status: NodeStatus['status']) => {
    switch (status) {
      case 'healthy':
        return '✅';
      case 'warning':
        return '⚠️';
      case 'error':
        return '❌';
      default:
        return '❓';
    }
  };

  const getUsageColor = (usage: number) => {
    if (usage > 80) return 'bg-red-500';
    if (usage > 60) return 'bg-yellow-500';
    return 'bg-green-500';
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
              <h1 className="text-2xl font-bold text-gray-900 dark:text-white">System Status</h1>
              <p className="text-gray-600 dark:text-gray-300">
                Monitor system health and performance
              </p>
            </div>
            {hasPermission('system', 'read') && (
              <button className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md font-medium">
                Refresh Status
              </button>
            )}
          </div>

          {loading ? (
            <div className="text-center py-8">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto mb-4"></div>
              <p className="text-gray-600 dark:text-gray-300">Loading system status...</p>
            </div>
          ) : error ? (
            <div className="text-center py-8">
              <p className="text-red-600 dark:text-red-400">{error}</p>
            </div>
          ) : (
            <>
              {/* System Overview */}
              {metrics && (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
                  <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
                    <div className="flex items-center">
                      <div className="flex-shrink-0">
                        <div className="w-8 h-8 bg-blue-500 rounded-full flex items-center justify-center">
                          <span className="text-white font-bold">N</span>
                        </div>
                      </div>
                      <div className="ml-5 w-0 flex-1">
                        <dl>
                          <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                            Total Nodes
                          </dt>
                          <dd className="text-lg font-medium text-gray-900 dark:text-white">
                            {metrics.totalNodes}
                          </dd>
                        </dl>
                      </div>
                    </div>
                  </div>

                  <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
                    <div className="flex items-center">
                      <div className="flex-shrink-0">
                        <div className="w-8 h-8 bg-green-500 rounded-full flex items-center justify-center">
                          <span className="text-white font-bold">A</span>
                        </div>
                      </div>
                      <div className="ml-5 w-0 flex-1">
                        <dl>
                          <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                            Active Apps
                          </dt>
                          <dd className="text-lg font-medium text-gray-900 dark:text-white">
                            {metrics.activeApplications}
                          </dd>
                        </dl>
                      </div>
                    </div>
                  </div>

                  <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
                    <div className="flex items-center">
                      <div className="flex-shrink-0">
                        <div className="w-8 h-8 bg-purple-500 rounded-full flex items-center justify-center">
                          <span className="text-white font-bold">C</span>
                        </div>
                      </div>
                      <div className="ml-5 w-0 flex-1">
                        <dl>
                          <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                            Avg CPU
                          </dt>
                          <dd className="text-lg font-medium text-gray-900 dark:text-white">
                            {metrics.avgCPUUsage}%
                          </dd>
                        </dl>
                      </div>
                    </div>
                  </div>

                  <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
                    <div className="flex items-center">
                      <div className="flex-shrink-0">
                        <div className="w-8 h-8 bg-orange-500 rounded-full flex items-center justify-center">
                          <span className="text-white font-bold">M</span>
                        </div>
                      </div>
                      <div className="ml-5 w-0 flex-1">
                        <dl>
                          <dt className="text-sm font-medium text-gray-500 dark:text-gray-400 truncate">
                            Avg Memory
                          </dt>
                          <dd className="text-lg font-medium text-gray-900 dark:text-white">
                            {metrics.avgMemoryUsage}%
                          </dd>
                        </dl>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Nodes List */}
              <div className="bg-white dark:bg-gray-800 shadow rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-4">
                    Node Status
                  </h3>

                  <div className="space-y-4">
                    {nodes.map((node) => (
                      <div key={node.id} className="border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                        <div className="flex items-center justify-between mb-4">
                          <div className="flex items-center space-x-3">
                            <span className="text-lg">{getStatusIcon(node.status)}</span>
                            <div>
                              <h4 className="text-lg font-medium text-gray-900 dark:text-white">
                                {node.name}
                              </h4>
                              <p className="text-sm text-gray-500 dark:text-gray-400">
                                Uptime: {node.uptime}
                              </p>
                            </div>
                          </div>
                          <div className="flex items-center space-x-4">
                            <span className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getStatusColor(node.status)}`}>
                              {node.status}
                            </span>
                            <span className="text-sm text-gray-500 dark:text-gray-400">
                              Last: {formatDate(node.lastHeartbeat)}
                            </span>
                          </div>
                        </div>

                        <div className="grid grid-cols-3 gap-4">
                          <div>
                            <div className="flex justify-between items-center mb-1">
                              <span className="text-sm font-medium text-gray-900 dark:text-white">CPU</span>
                              <span className="text-sm text-gray-600 dark:text-gray-300">{node.cpu}%</span>
                            </div>
                            <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                              <div
                                className={`h-2 rounded-full ${getUsageColor(node.cpu)}`}
                                style={{ width: `${node.cpu}%` }}
                              ></div>
                            </div>
                          </div>

                          <div>
                            <div className="flex justify-between items-center mb-1">
                              <span className="text-sm font-medium text-gray-900 dark:text-white">Memory</span>
                              <span className="text-sm text-gray-600 dark:text-gray-300">{node.memory}%</span>
                            </div>
                            <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                              <div
                                className={`h-2 rounded-full ${getUsageColor(node.memory)}`}
                                style={{ width: `${node.memory}%` }}
                              ></div>
                            </div>
                          </div>

                          <div>
                            <div className="flex justify-between items-center mb-1">
                              <span className="text-sm font-medium text-gray-900 dark:text-white">Disk</span>
                              <span className="text-sm text-gray-600 dark:text-gray-300">{node.disk}%</span>
                            </div>
                            <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                              <div
                                className={`h-2 rounded-full ${getUsageColor(node.disk)}`}
                                style={{ width: `${node.disk}%` }}
                              ></div>
                            </div>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </>
          )}
        </div>
      </DashboardLayout>
    </ProtectedRoute>
  );
}
