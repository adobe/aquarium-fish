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
import { useStreaming } from '../contexts/StreamingContext';
import { StreamingList, type ListColumn, type ListItemAction } from '../components/StreamingList';

import { create } from '@bufbuild/protobuf';
import { LabelServiceCreateRequestSchema, LabelServiceRemoveRequestSchema } from '../../gen/aquarium/v2/label_pb';
import type { Label } from '../../gen/aquarium/v2/label_pb';
import { LabelForm } from '../../gen/components';
import { PermService, PermLabel } from '../../gen/permissions/permissions_grpc';

export function meta() {
  return [
    { title: 'Labels - Aquarium Fish' },
    { name: 'description', content: 'Manage and monitor labels' },
  ];
}

export default function Labels() {
  const { hasPermission } = useAuth();
  const { data, sendRequest, fetchLabels } = useStreaming();

  // Labels state
  const [showCreateLabelModal, setShowCreateLabelModal] = useState(false);
  const [showLabelDetailsModal, setShowLabelDetailsModal] = useState(false);
  const [selectedLabel, setSelectedLabel] = useState<Label | null>(null);

  // Fetch data when component mounts
  useEffect(() => {
    fetchLabels();
  }, []);

  // Format timestamp
  const formatTimestamp = (timestamp: any) => {
    if (!timestamp) return 'Unknown';
    const date = new Date(Number(timestamp.seconds) * 1000);
    return date.toLocaleString();
  };

  // Label operations
  const handleCreateLabel = async (labelData: Label) => {
    try {
      // Create label request using streaming
      const labelRequest = create(LabelServiceCreateRequestSchema, {
        label: {
          ...labelData,
          uid: crypto.randomUUID(),
        },
      });

      console.log('Creating label:', labelRequest);
      await sendRequest(labelRequest, 'LabelServiceCreateRequest');
      setShowCreateLabelModal(false);
    } catch (error) {
      console.error(`Failed to create label: ${error}`);
    }
  };

  const handleRemoveLabel = async (label: Label) => {
    if (!confirm('Are you sure you want to delete this label?')) return;

    try {
      const removeRequest = create(LabelServiceRemoveRequestSchema, {
        labelUid: label.uid,
      });
      await sendRequest(removeRequest, 'LabelServiceRemoveRequest');
    } catch (error) {
      console.error(`Failed to delete label: ${error}`);
    }
  };

  // Define columns for the labels list
  const columns: ListColumn[] = [
    {
      key: 'name',
      label: 'Name & Version',
      filterable: true,
      render: (label: Label) => (
        <div>
          <div className="text-sm font-medium text-gray-900 dark:text-white">
            {label.name}:{label.version}
          </div>
          <div className="text-sm text-gray-500 dark:text-gray-400">
            Created: {formatTimestamp(label.createdAt)}
          </div>
        </div>
      ),
    },
  ];

  // Define actions for each label
  const actions: ListItemAction[] = [
    {
      label: 'View Details',
      onClick: (label: Label) => {
        setSelectedLabel(label);
        setShowLabelDetailsModal(true);
      },
      className: 'px-3 py-1 text-sm bg-blue-100 text-blue-800 rounded-md hover:bg-blue-200',
    },
    {
      label: 'Remove',
      onClick: handleRemoveLabel,
      className: 'px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200',
      permission: { resource: PermService.Label, action: PermLabel.Remove },
    },
  ];

  const canCreateLabel = hasPermission(PermService.Label, PermLabel.Create);

  return (
    <ProtectedRoute>
      <DashboardLayout>
        <div className="space-y-6">
          {/* Header */}
          <div className="flex justify-between items-center">
            <div>
              <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
                Labels
              </h1>
              <p className="text-gray-600 dark:text-gray-400">
                Manage and monitor labels
              </p>
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

          {/* Streaming Labels List */}
          <StreamingList
            objectType="labels"
            columns={columns}
            actions={actions}
            filterBy={['name']}
            sortBy={{ key: 'name', direction: 'asc' }}
            itemKey={(label: Label) => label.uid}
            onItemClick={(label: Label) => {
              setSelectedLabel(label);
              setShowLabelDetailsModal(true);
            }}
            permissions={{ list: { resource: PermService.Label, action: PermLabel.List } }}
            emptyMessage="No labels found"
          />
        </div>

        {/* Create Label Modal */}
        {showCreateLabelModal && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
              <LabelForm
                mode="create"
                onSubmit={handleCreateLabel}
                onCancel={() => setShowCreateLabelModal(false)}
                title="Create Label"
              />
            </div>
          </div>
        )}

        {/* Label Details Modal */}
        {showLabelDetailsModal && selectedLabel && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
              <div className="flex justify-between items-center mb-4">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                  Label Details: {selectedLabel.name}:{selectedLabel.version}
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
                {(() => {
                  const otherVersions = data.labels.filter(l =>
                    l.name === selectedLabel.name && l.uid !== selectedLabel.uid
                  );

                  return otherVersions.length > 0 && (
                    <div>
                      <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                        Other Versions
                      </h3>
                      <div className="space-y-2">
                        {otherVersions.map((label) => (
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
                  );
                })()}
              </div>
            </div>
          </div>
        )}
      </DashboardLayout>
    </ProtectedRoute>
  );
}
