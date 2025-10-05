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
import { useLabels, useLabelCreate, useLabelRemove } from '../hooks/useLabels';
import { StreamingList, type ListColumn, type ListItemAction } from '../../../components/StreamingList';
import { LabelForm } from '../../../../gen/components';
import { PermService, PermLabel } from '../../../../gen/permissions/permissions_grpc';
import type { Label } from '../../../../gen/aquarium/v2/label_pb';

// Format timestamp
const formatTimestamp = (timestamp: any) => {
  if (!timestamp) return 'Unknown';
  const date = new Date(Number(timestamp.seconds) * 1000);
  return date.toLocaleString();
};

export function LabelsPage() {
  const { hasPermission } = useAuth();
  const { fetchLabels, data } = useStreaming();
  useLabels();
  const { create } = useLabelCreate();
  const { remove } = useLabelRemove();

  const [showCreateLabelModal, setShowCreateLabelModal] = useState(false);
  const [showLabelDetailsModal, setShowLabelDetailsModal] = useState(false);
  const [showCopyLabelModal, setShowCopyLabelModal] = useState(false);
  const [selectedLabel, setSelectedLabel] = useState<Label | null>(null);
  const [labelToCopy, setLabelToCopy] = useState<Label | null>(null);

  // Fetch data when component mounts
  useEffect(() => {
    fetchLabels();
  }, [fetchLabels]);

  const handleCreateLabel = async (labelData: Label) => {
    try {
      console.debug('Creating:', labelData);
      await create(labelData);
      setShowCreateLabelModal(false);
    } catch (error) {
      console.error('Failed to create label:', error);
    }
  };

  const handleCopyLabel = async (labelData: Label) => {
    try {
      console.debug('Copying:', labelData);
      // Create a copy with a new uid (empty) and incremented version
      const copiedLabel = {
        ...labelData,
        uid: '', // Clear UID so backend creates a new one
        createdAt: undefined, // Clear timestamp
      };
      await create(copiedLabel);
      setShowCopyLabelModal(false);
      setLabelToCopy(null);
    } catch (error) {
      console.error('Failed to copy label:', error);
    }
  };

  const handleRemoveLabel = async (label: Label) => {
    if (!confirm('Are you sure you want to delete this label?')) return;

    try {
      await remove(label.uid);
    } catch (error) {
      console.error('Failed to delete label:', error);
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
      label: 'Copy',
      onClick: (label: Label) => {
        setLabelToCopy(label);
        setShowCopyLabelModal(true);
      },
      className: 'px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200',
      permission: { resource: PermService.Label, action: PermLabel.Create },
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
            <LabelForm
              mode="view"
              initialData={selectedLabel}
              onSubmit={() => {}}
              onCancel={() => setShowLabelDetailsModal(false)}
              title={`Label Details: ${selectedLabel.name}:${selectedLabel.version}`}
              readonly={true}
            />
          </div>
        </div>
      )}

      {/* Copy Label Modal */}
      {showCopyLabelModal && labelToCopy && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
            <LabelForm
              mode="create"
              initialData={labelToCopy}
              onSubmit={handleCopyLabel}
              onCancel={() => {
                setShowCopyLabelModal(false);
                setLabelToCopy(null);
              }}
              title={`Copy Label: ${labelToCopy.name}:${labelToCopy.version}`}
            />
          </div>
        </div>
      )}
    </div>
  );
}

