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
import { nodeServiceHelpers } from '../lib/services';
import type { Node } from '../../gen/aquarium/v2/node_pb';

export function meta() {
  return [
    { title: 'Node Status - Aquarium Fish' },
    { name: 'description', content: 'View node status and system information' },
  ];
}

export default function Status() {
  const { user, hasPermission } = useAuth();
  const [nodes, setNodes] = useState<Node[]>([]);
  const [thisNode, setThisNode] = useState<Node | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showDetailsModal, setShowDetailsModal] = useState(false);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const fetchNodes = async () => {
    try {
      setError(null);
      setRefreshing(true);

      const canListNodes = hasPermission('NodeService', 'List');
      const canGetThisNode = hasPermission('NodeService', 'GetThis');

      if (canListNodes) {
        const nodesList = await nodeServiceHelpers.list();
        setNodes(nodesList);
      }

      if (canGetThisNode) {
        const currentNode = await nodeServiceHelpers.getThis();
        setThisNode(currentNode);
      }

      setLoading(false);
    } catch (err) {
      setError(`Failed to fetch nodes: ${err}`);
      setLoading(false);
    } finally {
      setRefreshing(false);
    }
  };

  useEffect(() => {
    fetchNodes();
  }, []);

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const formatTimestamp = (timestamp: any) => {
    if (!timestamp) return 'Unknown';
    const date = new Date(Number(timestamp.seconds) * 1000);
    return date.toLocaleString();
  };

  const canListNodes = hasPermission('NodeService', 'List');
  const canGetThisNode = hasPermission('NodeService', 'GetThis');
  const canSetMaintenance = hasPermission('NodeService', 'SetMaintenance');

  if (!canListNodes && !canGetThisNode) {
    return (
      <ProtectedRoute>
        <DashboardLayout>
          <div className="text-center py-8">
            <p className="text-red-600 dark:text-red-400">
              You don't have permission to view node status.
            </p>
            <p className="text-gray-600 dark:text-gray-400 mt-2">
              Required permissions: NodeService.List or NodeService.GetThis
            </p>
          </div>
        </DashboardLayout>
      </ProtectedRoute>
    );
  }

  return (
    <ProtectedRoute>
      <DashboardLayout>
        <div className="space-y-6">
          {/* Header */}
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
              Node Status
            </h1>
            <p className="text-gray-600 dark:text-gray-400">
              Monitor node health and system information
            </p>
          </div>

          {/* Error */}
          {error && (
            <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-md">
              {error}
            </div>
          )}

          {/* Current Node */}
          {canGetThisNode && thisNode && (
            <div className="bg-white dark:bg-gray-800 shadow rounded-lg p-6">
              <h2 className="text-lg font-medium text-gray-900 dark:text-white mb-4">
                Current Node
              </h2>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                <div>
                  <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Name</p>
                  <p className="text-sm text-gray-900 dark:text-white">{thisNode.name}</p>
                </div>
                <div>
                  <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Location</p>
                  <p className="text-sm text-gray-900 dark:text-white">{thisNode.location}</p>
                </div>
                <div>
                  <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Address</p>
                  <p className="text-sm text-gray-900 dark:text-white">{thisNode.address}</p>
                </div>
                <div>
                  <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Created</p>
                  <p className="text-sm text-gray-900 dark:text-white">
                    {formatTimestamp(thisNode.createdAt)}
                  </p>
                </div>
                <div>
                  <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Updated</p>
                  <p className="text-sm text-gray-900 dark:text-white">
                    {formatTimestamp(thisNode.updatedAt)}
                  </p>
                </div>
              </div>

              {/* System Information */}
              {thisNode.definition && (
                <div className="mt-6 space-y-4">
                  <h3 className="text-md font-medium text-gray-900 dark:text-white">
                    System Information
                  </h3>

                  {/* Host Info */}
                  {thisNode.definition.host && (
                    <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                      <h4 className="font-medium text-gray-900 dark:text-white mb-2">Host</h4>
                      <div className="grid grid-cols-2 gap-2 text-sm">
                        <div>
                          <span className="font-medium">Hostname:</span> {thisNode.definition.host.hostname}
                        </div>
                        <div>
                          <span className="font-medium">OS:</span> {thisNode.definition.host.os}
                        </div>
                        <div>
                          <span className="font-medium">Platform:</span> {thisNode.definition.host.platform}
                        </div>
                        <div>
                          <span className="font-medium">Arch:</span> {thisNode.definition.host.kernelArch}
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Memory Info */}
                  {thisNode.definition.memory && (
                    <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                      <h4 className="font-medium text-gray-900 dark:text-white mb-2">Memory</h4>
                      <div className="grid grid-cols-2 gap-2 text-sm">
                        <div>
                          <span className="font-medium">Total:</span> {formatBytes(Number(thisNode.definition.memory.total))}
                        </div>
                        <div>
                          <span className="font-medium">Used:</span> {formatBytes(Number(thisNode.definition.memory.used))}
                        </div>
                        <div>
                          <span className="font-medium">Available:</span> {formatBytes(Number(thisNode.definition.memory.available))}
                        </div>
                        <div>
                          <span className="font-medium">Usage:</span> {thisNode.definition.memory.usedPercent?.toFixed(1)}%
                        </div>
                      </div>
                    </div>
                  )}

                  {/* CPU Info */}
                  {thisNode.definition.cpu && thisNode.definition.cpu.length > 0 && (
                    <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                      <h4 className="font-medium text-gray-900 dark:text-white mb-2">CPU</h4>
                      <div className="text-sm">
                        <div>
                          <span className="font-medium">Model:</span> {thisNode.definition.cpu[0].modelName}
                        </div>
                        <div>
                          <span className="font-medium">Cores:</span> {thisNode.definition.cpu[0].cores}
                        </div>
                        <div>
                          <span className="font-medium">Frequency:</span> {thisNode.definition.cpu[0].mhz?.toFixed(0)} MHz
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Disk Info */}
                  {thisNode.definition.disks && Object.keys(thisNode.definition.disks).length > 0 && (
                    <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                      <h4 className="font-medium text-gray-900 dark:text-white mb-2">Disks</h4>
                      <div className="space-y-2">
                        {Object.entries(thisNode.definition.disks).map(([path, disk]) => (
                          <div key={path} className="text-sm">
                            <div className="font-medium">{path}</div>
                            <div className="ml-2 text-gray-600 dark:text-gray-400">
                              {formatBytes(Number(disk.used))} / {formatBytes(Number(disk.total))} ({disk.usedPercent?.toFixed(1)}%)
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Network Info */}
                  {thisNode.definition.nets && thisNode.definition.nets.length > 0 && (
                    <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                      <h4 className="font-medium text-gray-900 dark:text-white mb-2">Network</h4>
                      <div className="space-y-2">
                        {thisNode.definition.nets.map((net, index) => (
                          <div key={index} className="text-sm">
                            <div className="font-medium">{net.name}</div>
                            <div className="ml-2 text-gray-600 dark:text-gray-400">
                              {net.addrs?.join(', ')}
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* All Nodes */}
          {canListNodes && (
            <div className="bg-white dark:bg-gray-800 shadow rounded-lg">
              <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                <h2 className="text-lg font-medium text-gray-900 dark:text-white">
                  All Nodes
                </h2>
              </div>

              {loading ? (
                <div className="p-6 text-center">
                  <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
                  <p className="mt-2 text-gray-600 dark:text-gray-400">Loading nodes...</p>
                </div>
              ) : nodes.length === 0 ? (
                <div className="p-6 text-center text-gray-500 dark:text-gray-400">
                  No nodes found
                </div>
              ) : (
                <div className="divide-y divide-gray-200 dark:divide-gray-700">
                  {nodes.map((node) => (
                    <div
                      key={node.uid}
                      className="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-700 cursor-pointer"
                      onClick={() => {
                        setSelectedNode(node);
                        setShowDetailsModal(true);
                      }}
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex items-center space-x-4">
                          <div className="w-3 h-3 bg-green-500 rounded-full" />
                          <div>
                            <div className="text-sm font-medium text-gray-900 dark:text-white">
                              {node.name}
                            </div>
                            <div className="text-sm text-gray-500 dark:text-gray-400">
                              {node.location} • {node.address}
                            </div>
                          </div>
                        </div>
                        <div className="text-sm text-gray-500 dark:text-gray-400">
                          Updated: {formatTimestamp(node.updatedAt)}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Node Details Modal */}
        {showDetailsModal && selectedNode && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
              <div className="flex justify-between items-center mb-4">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                  Node Details: {selectedNode.name}
                </h2>
                <button
                  onClick={() => setShowDetailsModal(false)}
                  className="text-gray-500 hover:text-gray-700"
                >
                  ×
                </button>
              </div>

              <div className="space-y-6">
                {/* Basic Information */}
                <div>
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                    Basic Information
                  </h3>
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">UID</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedNode.uid}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Name</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedNode.name}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Location</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedNode.location}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Address</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedNode.address}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Created</p>
                      <p className="text-sm text-gray-900 dark:text-white">
                        {formatTimestamp(selectedNode.createdAt)}
                      </p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Updated</p>
                      <p className="text-sm text-gray-900 dark:text-white">
                        {formatTimestamp(selectedNode.updatedAt)}
                      </p>
                    </div>
                  </div>
                </div>

                {/* System Details */}
                {selectedNode.definition && (
                  <div>
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                      System Details
                    </h3>

                    {/* Host Information */}
                    {selectedNode.definition.host && (
                      <div className="mb-4">
                        <h4 className="font-medium text-gray-900 dark:text-white mb-2">Host Information</h4>
                        <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                          <div className="grid grid-cols-2 gap-4 text-sm">
                            <div>
                              <span className="font-medium">Hostname:</span> {selectedNode.definition.host.hostname}
                            </div>
                            <div>
                              <span className="font-medium">OS:</span> {selectedNode.definition.host.os}
                            </div>
                            <div>
                              <span className="font-medium">Platform:</span> {selectedNode.definition.host.platform}
                            </div>
                            <div>
                              <span className="font-medium">Platform Family:</span> {selectedNode.definition.host.platformFamily}
                            </div>
                            <div>
                              <span className="font-medium">Platform Version:</span> {selectedNode.definition.host.platformVersion}
                            </div>
                            <div>
                              <span className="font-medium">Kernel Version:</span> {selectedNode.definition.host.kernelVersion}
                            </div>
                            <div>
                              <span className="font-medium">Kernel Arch:</span> {selectedNode.definition.host.kernelArch}
                            </div>
                          </div>
                        </div>
                      </div>
                    )}

                    {/* Memory Information */}
                    {selectedNode.definition.memory && (
                      <div className="mb-4">
                        <h4 className="font-medium text-gray-900 dark:text-white mb-2">Memory Information</h4>
                        <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                          <div className="grid grid-cols-2 gap-4 text-sm">
                            <div>
                              <span className="font-medium">Total:</span> {formatBytes(Number(selectedNode.definition.memory.total))}
                            </div>
                            <div>
                              <span className="font-medium">Used:</span> {formatBytes(Number(selectedNode.definition.memory.used))}
                            </div>
                            <div>
                              <span className="font-medium">Available:</span> {formatBytes(Number(selectedNode.definition.memory.available))}
                            </div>
                            <div>
                              <span className="font-medium">Usage:</span> {selectedNode.definition.memory.usedPercent?.toFixed(1)}%
                            </div>
                          </div>
                        </div>
                      </div>
                    )}

                    {/* CPU Information */}
                    {selectedNode.definition.cpu && selectedNode.definition.cpu.length > 0 && (
                      <div className="mb-4">
                        <h4 className="font-medium text-gray-900 dark:text-white mb-2">CPU Information</h4>
                        <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                          {selectedNode.definition.cpu.map((cpu, index) => (
                            <div key={index} className="mb-2 last:mb-0">
                              <div className="text-sm">
                                <div><span className="font-medium">CPU {index + 1}:</span> {cpu.modelName}</div>
                                <div className="ml-2 text-gray-600 dark:text-gray-400">
                                  Cores: {cpu.cores} • Frequency: {cpu.mhz?.toFixed(0)} MHz • Cache: {cpu.cacheSize}
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Disk Information */}
                    {selectedNode.definition.disks && Object.keys(selectedNode.definition.disks).length > 0 && (
                      <div className="mb-4">
                        <h4 className="font-medium text-gray-900 dark:text-white mb-2">Disk Information</h4>
                        <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                          <div className="space-y-2">
                            {Object.entries(selectedNode.definition.disks).map(([path, disk]) => (
                              <div key={path} className="text-sm">
                                <div className="font-medium">{path}</div>
                                <div className="ml-2 text-gray-600 dark:text-gray-400">
                                  Type: {disk.fstype} • Used: {formatBytes(Number(disk.used))} / {formatBytes(Number(disk.total))} ({disk.usedPercent?.toFixed(1)}%)
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    )}

                    {/* Network Information */}
                    {selectedNode.definition.nets && selectedNode.definition.nets.length > 0 && (
                      <div className="mb-4">
                        <h4 className="font-medium text-gray-900 dark:text-white mb-2">Network Information</h4>
                        <div className="bg-gray-50 dark:bg-gray-700 rounded-md p-4">
                          <div className="space-y-2">
                            {selectedNode.definition.nets.map((net, index) => (
                              <div key={index} className="text-sm">
                                <div className="font-medium">{net.name}</div>
                                <div className="ml-2 text-gray-600 dark:text-gray-400">
                                  Addresses: {net.addrs?.join(', ')}
                                  <br />
                                  Flags: {net.flags?.join(', ')}
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
      </DashboardLayout>
    </ProtectedRoute>
  );
}
