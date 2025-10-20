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
import { useLabels, useLabelCreate, useLabelUpdate, useLabelRemove } from '../hooks/useLabels';
import { StreamingList, type ListColumn, type ListItemAction } from '../../../components/StreamingList';
import { LabelForm } from '../../../../gen/components';
import { Modal } from '../../../components/Modal';
import { PermService, PermLabel } from '../../../../gen/permissions/permissions_grpc';
import type { Label } from '../../../../gen/aquarium/v2/label_pb';

// Format timestamp
const formatTimestamp = (timestamp: any) => {
  if (!timestamp) return 'Unknown';
  const date = new Date(Number(timestamp.seconds) * 1000);
  return date.toLocaleString();
};

export function LabelsPage() {
  const { hasPermission, user } = useAuth();
  const { fetchLabels, data } = useStreaming();
  useLabels();
  const { create } = useLabelCreate();
  const { update } = useLabelUpdate();
  const { remove } = useLabelRemove();

  const [showCreateLabelModal, setShowCreateLabelModal] = useState(false);
  const [showLabelDetailsModal, setShowLabelDetailsModal] = useState(false);
  const [showEditLabelModal, setShowEditLabelModal] = useState(false);
  const [showCopyLabelModal, setShowCopyLabelModal] = useState(false);
  const [showVersionZeroConfirm, setShowVersionZeroConfirm] = useState(false);
  const [pendingLabelData, setPendingLabelData] = useState<Label | null>(null);
  const [selectedLabel, setSelectedLabel] = useState<Label | null>(null);
  const [labelToCopy, setLabelToCopy] = useState<Label | null>(null);
  const [hasCreateFormChanges, setHasCreateFormChanges] = useState(false);
  const [hasEditFormChanges, setHasEditFormChanges] = useState(false);
  const [hasCopyFormChanges, setHasCopyFormChanges] = useState(false);

  // Fetch data when component mounts
  useEffect(() => {
    fetchLabels();
  }, [fetchLabels]);

  // Check if user can edit a label (is owner and has Update permission, or has UpdateAll permission)
  const canEditLabel = (label: Label): boolean => {
    if (!user) return false;

    // User needs to have Update or UpdateAll permission
    const hasUpdatePermission = hasPermission(PermService.Label, PermLabel.Update);
    const hasUpdateAllPermission = hasPermission(PermService.Label, PermLabel.UpdateAll);

    if (!hasUpdatePermission && !hasUpdateAllPermission) {
      return false;
    }

    // If user has UpdateAll permission, they can edit any label with version 0
    if (hasUpdateAllPermission && label.version === 0) {
      return true;
    }

    // If user has Update permission, they can only edit their own labels with version 0
    if (hasUpdatePermission && label.version === 0 && label.ownerName === user.name) {
      return true;
    }

    return false;
  };

  const handleCreateLabel = async (labelData: Label) => {
    // Check if version is 0 and ask for confirmation
    if (labelData.version === 0) {
      setPendingLabelData(labelData);
      setShowVersionZeroConfirm(true);
      return;
    }

    try {
      console.debug('Creating:', labelData);
      await create(labelData);
      setShowCreateLabelModal(false);
    } catch (error) {
      console.error('Failed to create label:', error);
    }
  };

  const handleConfirmVersionZero = async () => {
    if (!pendingLabelData) return;

    try {
      console.debug('Creating editable label:', pendingLabelData);
      await create(pendingLabelData);
      setShowCreateLabelModal(false);
      setShowVersionZeroConfirm(false);
      setPendingLabelData(null);
    } catch (error) {
      console.error('Failed to create label:', error);
    }
  };

  const handleCancelVersionZero = () => {
    setShowVersionZeroConfirm(false);
    setPendingLabelData(null);
    // Keep the create modal open so user can change the version
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

  const handleUpdateLabel = async (labelData: Label) => {
    try {
      console.debug('Updating:', labelData);
      await update(labelData);
      setShowEditLabelModal(false);
      setSelectedLabel(null);
    } catch (error) {
      console.error('Failed to update label:', error);
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
      label: 'Edit',
      onClick: (label: Label) => {
        setSelectedLabel(label);
        setShowEditLabelModal(true);
      },
      className: 'px-3 py-1 text-sm bg-purple-100 text-purple-800 rounded-md hover:bg-purple-200',
      // Only show Edit if user can edit this label (version=0 and proper permissions)
      condition: (label: Label) => canEditLabel(label),
    },
    {
      label: 'View',
      onClick: (label: Label) => {
        setSelectedLabel(label);
        setShowLabelDetailsModal(true);
      },
      className: 'px-3 py-1 text-sm bg-blue-100 text-blue-800 rounded-md hover:bg-blue-200',
      // Only show View if user cannot edit this label
      condition: (label: Label) => !canEditLabel(label),
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
      <Modal
        isOpen={showCreateLabelModal}
        onClose={() => setShowCreateLabelModal(false)}
        hasUnsavedChanges={hasCreateFormChanges}
      >
        <LabelForm
          mode="create"
          onSubmit={handleCreateLabel}
          onCancel={() => setShowCreateLabelModal(false)}
          onFormChange={setHasCreateFormChanges}
          title="Create Label"
        />
      </Modal>

      {/* Label Details Modal */}
      {selectedLabel && (
        <Modal
          isOpen={showLabelDetailsModal}
          onClose={() => setShowLabelDetailsModal(false)}
          hasUnsavedChanges={false}
        >
          <LabelForm
            mode="view"
            initialData={selectedLabel}
            onSubmit={() => {}}
            onCancel={() => setShowLabelDetailsModal(false)}
            title={`Label Details: ${selectedLabel.name}:${selectedLabel.version}`}
            readonly={true}
          />
        </Modal>
      )}

      {/* Edit Label Modal */}
      {selectedLabel && (
        <Modal
          isOpen={showEditLabelModal}
          onClose={() => {
            setShowEditLabelModal(false);
            setSelectedLabel(null);
          }}
          hasUnsavedChanges={hasEditFormChanges}
        >
          <LabelForm
            mode="edit"
            initialData={selectedLabel}
            onSubmit={handleUpdateLabel}
            onCancel={() => {
              setShowEditLabelModal(false);
              setSelectedLabel(null);
            }}
            onFormChange={setHasEditFormChanges}
            title={`Edit Label: ${selectedLabel.name}:${selectedLabel.version}`}
          />
        </Modal>
      )}

      {/* Copy Label Modal */}
      {labelToCopy && (
        <Modal
          isOpen={showCopyLabelModal}
          onClose={() => {
            setShowCopyLabelModal(false);
            setLabelToCopy(null);
          }}
          hasUnsavedChanges={hasCopyFormChanges}
        >
          <LabelForm
            mode="create"
            initialData={labelToCopy}
            onSubmit={handleCopyLabel}
            onCancel={() => {
              setShowCopyLabelModal(false);
              setLabelToCopy(null);
            }}
            onFormChange={setHasCopyFormChanges}
            title={`Copy Label: ${labelToCopy.name}:${labelToCopy.version}`}
          />
        </Modal>
      )}

      {/* Version Zero Confirmation Modal */}
      {showVersionZeroConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
            <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">
              Create Editable Label?
            </h2>
            <p className="text-gray-600 dark:text-gray-400 mb-6">
              You are creating a label with version = 0. This label will be editable but temporary.
              Editable labels must have a removal date set and will be automatically removed after that date.
            </p>
            <p className="text-gray-600 dark:text-gray-400 mb-6">
              <strong>Do you want to create this editable label?</strong>
            </p>
            <div className="flex justify-end space-x-3">
              <button
                onClick={handleCancelVersionZero}
                className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300"
              >
                No, go back
              </button>
              <button
                onClick={handleConfirmVersionZero}
                className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700"
              >
                Yes, create editable label
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

