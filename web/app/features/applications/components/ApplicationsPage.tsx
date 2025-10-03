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
import { useApplications, useApplicationCreate, useApplicationDeallocate, useApplicationSSH } from '../hooks/useApplications';
import { StreamingList, type ListColumn, type ListItemAction } from '../../../components/StreamingList';
import { ApplicationForm, ApplicationResourceForm } from '../../../../gen/components';
import { timestampToDate } from '../../../lib/auth';
import { PermService, PermApplication } from '../../../../gen/permissions/permissions_grpc';
import type { ApplicationWithDetails } from '../types';

// Helper to get status text
function getStatusText(state?: any) {
  if (!state) return 'Unknown';
  switch (state.status) {
    case 1: return 'New';
    case 2: return 'Elected';
    case 3: return 'Allocated';
    case 4: return 'Deallocate';
    case 5: return 'Deallocated';
    case 6: return 'Error';
    default: return 'Unknown';
  }
}

// Helper to get status color
function getStatusColor(state?: any) {
  if (!state) return 'bg-gray-500';
  switch (state.status) {
    case 1: return 'bg-blue-500';
    case 2: return 'bg-yellow-500';
    case 3: return 'bg-green-500';
    case 4: return 'bg-orange-500';
    case 5: return 'bg-gray-500';
    case 6: return 'bg-red-500';
    default: return 'bg-gray-500';
  }
}

export function ApplicationsPage() {
  const { hasPermission } = useAuth();
  const { fetchApplications, fetchLabels } = useStreaming();
  const { applications, labels } = useApplications();
  const { create } = useApplicationCreate();
  const { deallocate } = useApplicationDeallocate();
  const { getResourceAccess } = useApplicationSSH();

  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showDetailsModal, setShowDetailsModal] = useState(false);
  const [selectedApp, setSelectedApp] = useState<ApplicationWithDetails | null>(null);
  const [showSSHModal, setShowSSHModal] = useState(false);
  const [sshCredentials, setSSHCredentials] = useState<any>(null);

  // Fetch data when component mounts
  useEffect(() => {
    fetchApplications();
  }, [fetchApplications]);

  const handleCreateApplication = async (applicationData: any) => {
    try {
      await create(applicationData);
      setShowCreateModal(false);
    } catch (error) {
      console.error('Failed to create application:', error);
    }
  };

  const handleDeallocateApplication = async (app: ApplicationWithDetails) => {
    if (!confirm('Are you sure you want to deallocate this application?')) return;

    try {
      await deallocate(app.uid);
    } catch (error) {
      console.error('Failed to deallocate application:', error);
    }
  };

  const handleGetResourceAccess = async (app: ApplicationWithDetails) => {
    if (!app.resource) {
      console.error('No resource available for this application');
      return;
    }

    try {
      const credentials = await getResourceAccess(app.resource.uid);
      setSSHCredentials(credentials);
      setShowSSHModal(true);
    } catch (error) {
      console.error('Failed to get resource access:', error);
    }
  };

  // Define columns for the applications list
  const columns: ListColumn[] = [
    {
      key: 'name',
      label: 'Application',
      filterable: true,
      render: (app: ApplicationWithDetails) => {
        const label = labels.find(l => l.uid === app.labelUid);
        const labelName = label ? `${label.name}:${label.version}` : app.labelUid || 'Unknown Label';

        return (
          <div>
            <div className="text-sm font-medium text-gray-900 dark:text-white">
              {labelName} - {app.uid}
              {app.isUserOwned && (
                <span className="ml-2 px-2 py-1 text-xs bg-blue-100 text-blue-800 rounded-full">
                  Mine
                </span>
              )}
            </div>
            <div className="text-sm text-gray-500 dark:text-gray-400">
              Owner: {app.ownerName || 'Unknown'}
            </div>
          </div>
        );
      },
    },
    {
      key: 'status',
      label: 'Status',
      render: (app: ApplicationWithDetails) => (
        <div className="flex items-center">
          <div className={`w-3 h-3 rounded-full ${getStatusColor(app.state)} mr-2`} />
          <span className="text-sm text-gray-900 dark:text-white">
            {getStatusText(app.state)}
          </span>
        </div>
      ),
    },
    {
      key: 'created',
      label: 'Created',
      render: (app: ApplicationWithDetails) => (
        <span className="text-sm text-gray-500 dark:text-gray-400">
          {app.createdAt ? new Date(Number(app.createdAt.seconds) * 1000).toLocaleString() : 'Unknown'}
        </span>
      ),
    },
  ];

  // Define actions for each application
  const actions: ListItemAction[] = [
    {
      label: 'SSH Access',
      onClick: handleGetResourceAccess,
      className: 'px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200',
      condition: (app: ApplicationWithDetails) => !!app.resource,
    },
    {
      label: 'Deallocate',
      onClick: handleDeallocateApplication,
      className: 'px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200',
      condition: (app: ApplicationWithDetails) => app.isUserOwned || hasPermission(PermService.Application, PermApplication.DeallocateAll),
    },
  ];

  const canCreateApp = hasPermission(PermService.Application, PermApplication.Create);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
            Applications
          </h1>
          <p className="text-gray-600 dark:text-gray-400">
            Manage and monitor your applications
          </p>
        </div>
        <div className="flex items-center space-x-4">
          <button
            onClick={() => {
              fetchLabels();
              setShowCreateModal(true);
            }}
            disabled={!canCreateApp}
            className={`px-4 py-2 rounded-md text-white ${
              canCreateApp
                ? 'bg-blue-600 hover:bg-blue-700'
                : 'bg-gray-400 cursor-not-allowed'
            }`}
            title={!canCreateApp ? 'You need ApplicationService.Create permission' : ''}
          >
            Create Application
          </button>
        </div>
      </div>

      {/* Streaming Applications List */}
      <StreamingList
        objectType="applications"
        customData={applications}
        columns={columns}
        actions={actions}
        filterBy={['name']}
        sortBy={{ key: 'created', direction: 'desc' }}
        itemKey={(app: ApplicationWithDetails) => app.uid}
        onItemClick={(app: ApplicationWithDetails) => {
          fetchLabels();
          setSelectedApp(app);
          setShowDetailsModal(true);
        }}
        permissions={{ list: { resource: PermService.Application, action: PermApplication.List } }}
        emptyMessage="No applications found"
      />

      {/* Create Application Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
            <ApplicationForm
              mode="create"
              onSubmit={handleCreateApplication}
              onCancel={() => setShowCreateModal(false)}
              title="Create Application"
            />
          </div>
        </div>
      )}

      {/* Application Details Modal */}
      {showDetailsModal && selectedApp && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                Application Details: {selectedApp.uid}
              </h2>
              <button
                onClick={() => setShowDetailsModal(false)}
                className="text-gray-500 hover:text-gray-700"
              >
                ×
              </button>
            </div>
            <ApplicationForm
              mode="view"
              initialData={selectedApp}
              onSubmit={() => {}}
              onCancel={() => setShowDetailsModal(false)}
            />
            {/* Show ApplicationState and ApplicationResource if available */}
            <div className="mt-6">
              {selectedApp.state && (
                <div className="mb-4">
                  <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">Current State</h3>
                  <div className="space-y-1 text-sm">
                    <div>
                      <span className="font-medium">Created at:</span> {selectedApp.state?.createdAt ? timestampToDate(selectedApp.state.createdAt).toLocaleString() : 'Unknown'}
                    </div>
                    <div>
                      <span className="font-medium">Status:</span> {getStatusText(selectedApp.state)}
                    </div>
                    <div>
                      <span className="font-medium">Description:</span> {selectedApp.state?.description || '—'}
                    </div>
                  </div>
                </div>
              )}
              {selectedApp.resource && (
                <div className="mb-4">
                  <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">Resource</h3>
                  <ApplicationResourceForm
                    mode="view"
                    initialData={selectedApp.resource}
                    onSubmit={() => {}}
                    onCancel={() => {}}
                    title="Application Resource"
                  />
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* SSH Resource Access Modal */}
      {showSSHModal && sshCredentials && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-2xl max-h-[90vh] overflow-y-auto">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                SSH Resource Access
              </h2>
              <button
                onClick={() => {
                  setShowSSHModal(false);
                  setSSHCredentials(null);
                }}
                className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                ×
              </button>
            </div>

            <div className="space-y-4">
              <div>
                <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">Connection Details</h3>
                <div className="space-y-2">
                  <div>
                    <span className="font-medium text-gray-700 dark:text-gray-300">Command:</span>
                    <div className="mt-1 p-3 bg-gray-100 dark:bg-gray-700 rounded font-mono text-sm">
                      ssh -p {sshCredentials.address?.split(':')[1] || '22'} {sshCredentials.username}@{sshCredentials.address?.split(':')[0] || 'localhost'}
                    </div>
                  </div>
                  <div>
                    <span className="font-medium text-gray-700 dark:text-gray-300">Username:</span>
                    <div className="mt-1 p-2 bg-gray-100 dark:bg-gray-700 rounded font-mono text-sm">
                      {sshCredentials.username}
                    </div>
                  </div>
                  <div>
                    <span className="font-medium text-gray-700 dark:text-gray-300">Password:</span>
                    <div className="mt-1 p-2 bg-gray-100 dark:bg-gray-700 rounded font-mono text-sm">
                      {sshCredentials.password}
                    </div>
                  </div>
                  <div>
                    <span className="font-medium text-gray-700 dark:text-gray-300">Private Key:</span>
                    <div className="mt-1 p-3 bg-gray-100 dark:bg-gray-700 rounded font-mono text-xs overflow-x-auto">
                      <pre className="whitespace-pre-wrap">{sshCredentials.key}</pre>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

