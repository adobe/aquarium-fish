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
import { useNodes, useThisNode, useNodeMaintenance } from '../hooks/useNodes';
import { StreamingList, type ListColumn, type ListItemAction } from '../../../components/StreamingList';
import { PermService, PermNode } from '../../../../gen/permissions/permissions_grpc';
import type { Node } from '../../../../gen/aquarium/v2/node_pb';

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

export function StatusPage() {
  const { hasPermission } = useAuth();
  const { fetchNodes } = useStreaming();
  useNodes();
  const { thisNode, fetch: fetchThisNode } = useThisNode();
  const { setMaintenance, isLoading: maintenanceLoading } = useNodeMaintenance();

  const [showDetailsModal, setShowDetailsModal] = useState(false);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [showMaintenanceModal, setShowMaintenanceModal] = useState(false);
  const [showShutdownModal, setShowShutdownModal] = useState(false);
  const [shutdownDelay, setShutdownDelay] = useState('1m');

  useEffect(() => {
    fetchThisNode();
    fetchNodes();
  }, [fetchThisNode, fetchNodes]);

  const handleMaintenanceToggle = async (enable: boolean) => {
    const success = await setMaintenance(enable);
    if (success) {
      setShowMaintenanceModal(false);
      await fetchThisNode();
    }
  };

  const handleShutdown = async () => {
    const success = await setMaintenance(undefined, true, shutdownDelay);
    if (success) {
      setShowShutdownModal(false);
      await fetchThisNode();
    }
  };

  // Define columns for nodes list
  const nodeColumns: ListColumn[] = [
    {
      key: 'name',
      label: 'Node',
      filterable: true,
      render: (node: Node) => (
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
      ),
    },
    {
      key: 'updated',
      label: 'Last Updated',
      render: (node: Node) => (
        <span className="text-sm text-gray-500 dark:text-gray-400">
          {formatTimestamp(node.updatedAt)}
        </span>
      ),
    },
  ];

  // Define actions for nodes
  const nodeActions: ListItemAction[] = [
    {
      label: 'View Details',
      onClick: (node: Node) => {
        setSelectedNode(node);
        setShowDetailsModal(true);
      },
      className: 'px-3 py-1 text-sm bg-blue-100 text-blue-800 rounded-md hover:bg-blue-200',
    },
  ];

  const canListNodes = hasPermission(PermService.Node, PermNode.List);
  const canGetThisNode = hasPermission(PermService.Node, PermNode.GetThis);
  const canSetMaintenance = hasPermission(PermService.Node, PermNode.SetMaintenance);

  if (!canListNodes && !canGetThisNode) {
    return (
      <div className="text-center py-8">
        <p className="text-red-600 dark:text-red-400">
          You don't have permission to view node status.
        </p>
        <p className="text-gray-600 dark:text-gray-400 mt-2">
          Required permissions: NodeService.List or NodeService.GetThis
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
            Node Status
          </h1>
          <p className="text-gray-600 dark:text-gray-400">
            View system information and node status
          </p>
        </div>
        <div className="flex items-center space-x-2">
          {canSetMaintenance && (
            <>
              <button
                onClick={() => setShowMaintenanceModal(true)}
                disabled={maintenanceLoading}
                className="px-4 py-2 bg-yellow-600 text-white rounded-md hover:bg-yellow-700 disabled:opacity-50"
              >
                {maintenanceLoading ? 'Loading...' : 'Maintenance Mode'}
              </button>
              <button
                onClick={() => setShowShutdownModal(true)}
                disabled={maintenanceLoading}
                className="px-4 py-2 bg-red-600 text-white rounded-md hover:bg-red-700 disabled:opacity-50"
              >
                {maintenanceLoading ? 'Loading...' : 'Shutdown Node'}
              </button>
            </>
          )}
        </div>
      </div>

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
        <div className="space-y-4">
          <h2 className="text-lg font-medium text-gray-900 dark:text-white">
            All Nodes
          </h2>

          <StreamingList
            objectType="nodes"
            columns={nodeColumns}
            actions={nodeActions}
            filterBy={['name']}
            itemKey={(node: Node) => node.uid}
            onItemClick={(node: Node) => {
              setSelectedNode(node);
              setShowDetailsModal(true);
            }}
            permissions={{ list: { resource: PermService.Node, action: PermNode.List } }}
            emptyMessage="No nodes found"
          />
        </div>
      )}

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

              {/* System Details - Similar structure as Current Node */}
              {selectedNode.definition && (
                <div>
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                    System Details
                  </h3>
                  {/* Include all the same sections as Current Node here */}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Maintenance Mode Modal */}
      {showMaintenanceModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
            <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-white">
              Maintenance Mode
            </h2>
            <p className="text-gray-600 dark:text-gray-400 mb-6">
              Choose whether to enable or disable maintenance mode for this node.
            </p>
            <div className="flex justify-end space-x-3">
              <button
                onClick={() => setShowMaintenanceModal(false)}
                className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
              >
                Cancel
              </button>
              <button
                onClick={() => handleMaintenanceToggle(false)}
                disabled={maintenanceLoading}
                className="px-4 py-2 text-sm bg-green-600 text-white rounded-md hover:bg-green-700 disabled:opacity-50"
              >
                Disable
              </button>
              <button
                onClick={() => handleMaintenanceToggle(true)}
                disabled={maintenanceLoading}
                className="px-4 py-2 text-sm bg-yellow-600 text-white rounded-md hover:bg-yellow-700 disabled:opacity-50"
              >
                Enable
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Shutdown Modal */}
      {showShutdownModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
            <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-white">
              Shutdown Node
            </h2>
            <p className="text-gray-600 dark:text-gray-400 mb-4">
              This will put the node in maintenance mode and then shut it down after the specified delay.
            </p>
            <div className="mb-6">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Shutdown Delay
              </label>
              <input
                type="text"
                value={shutdownDelay}
                onChange={(e) => setShutdownDelay(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                placeholder="1m"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Format: 1h30m (hours, minutes, seconds)
              </p>
            </div>
            <div className="flex justify-end space-x-3">
              <button
                onClick={() => setShowShutdownModal(false)}
                className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
              >
                Cancel
              </button>
              <button
                onClick={handleShutdown}
                disabled={maintenanceLoading}
                className="px-4 py-2 text-sm bg-red-600 text-white rounded-md hover:bg-red-700 disabled:opacity-50"
              >
                Shutdown
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

