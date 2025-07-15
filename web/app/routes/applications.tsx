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
import { create } from '@bufbuild/protobuf';
import { ApplicationServiceCreateRequestSchema, type Application, type ApplicationState, type ApplicationResource } from '../../gen/aquarium/v2/application_pb';
import { utils } from '../lib/services';
import * as yaml from 'js-yaml';

export function meta() {
  return [
    { title: 'Applications - Aquarium Fish' },
    { name: 'description', content: 'Manage and monitor applications' },
  ];
}

interface ApplicationWithDetails extends Application {
  state?: ApplicationState;
  resource?: ApplicationResource;
  isUserOwned?: boolean;
}

export default function Applications() {
  const { user, hasPermission } = useAuth();
  const { data, isConnected, connectionStatus } = useStreaming();
  const [applications, setApplications] = useState<ApplicationWithDetails[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showDetailsModal, setShowDetailsModal] = useState(false);
  const [selectedApp, setSelectedApp] = useState<ApplicationWithDetails | null>(null);
  const [sortField, setSortField] = useState<'name' | 'created' | 'status'>('created');
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('desc');
  const [filterStatus, setFilterStatus] = useState<string>('all');
  const [filterOwner, setFilterOwner] = useState<string>('all');
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [createYaml, setCreateYaml] = useState('');
  const [yamlError, setYamlError] = useState<string | null>(null);

  // Process streaming data
  useEffect(() => {
    const processedApps = data.applications.map(app => {
      const state = data.applicationStates.get(app.uid);
      const resource = data.applicationResources.get(app.uid);
      const isUserOwned = user?.userName === app.ownerName;

      return {
        ...app,
        state,
        resource,
        isUserOwned,
      };
    });

    // Sort applications - prioritize user-owned ones first
    const sortedApps = processedApps.sort((a, b) => {
      // First priority: user-owned applications
      if (a.isUserOwned && !b.isUserOwned) return -1;
      if (!a.isUserOwned && b.isUserOwned) return 1;

      // Second priority: selected sort field
      const aValue = getSortValue(a, sortField);
      const bValue = getSortValue(b, sortField);

      if (sortDirection === 'asc') {
        return aValue < bValue ? -1 : aValue > bValue ? 1 : 0;
      } else {
        return aValue > bValue ? -1 : aValue < bValue ? 1 : 0;
      }
    });

    setApplications(sortedApps);
    setLoading(false);
  }, [data, user, sortField, sortDirection]);

  const getSortValue = (app: ApplicationWithDetails, field: string) => {
    switch (field) {
      case 'name':
        return app.labelUid || '';
      case 'created':
        return app.createdAt?.seconds || '0';
      case 'status':
        return app.state?.status || 0;
      default:
        return '';
    }
  };

  const getStatusText = (state?: ApplicationState) => {
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
  };

  const getStatusColor = (state?: ApplicationState) => {
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
  };

    const handleCreateApplication = async () => {
    try {
      setYamlError(null);

      // Parse and validate YAML
      const appData = yaml.load(createYaml) as any;

      if (!appData) {
        throw new Error('Invalid YAML format');
      }

      if (!appData.labelUid) {
        throw new Error('labelUid is required');
      }

      // Validate YAML structure
      if (appData.metadata && typeof appData.metadata !== 'object') {
        throw new Error('metadata must be an object');
      }

      console.log('Creating application:', {
        uid: crypto.randomUUID(),
        ownerName: user?.userName || '',
        labelUid: appData.labelUid,
        metadata: appData.metadata || {},
      });

      const application = create(ApplicationServiceCreateRequestSchema, {
        application: {
          uid: crypto.randomUUID(),
          ownerName: user?.userName || '',
          labelUid: appData.labelUid,
          metadata: appData.metadata || {},
        },
      });
      await sendRequest(application, 'ApplicationServiceCreateRequest');

      setShowCreateModal(false);
      setCreateYaml('');
    } catch (error) {
      if (error instanceof yaml.YAMLException) {
        setYamlError(`YAML parsing error: ${error.message}`);
      } else {
        setYamlError(`Failed to create application: ${error}`);
      }
    }
  };

  const handleDeallocateApplication = async (uid: string) => {
    if (!confirm('Are you sure you want to deallocate this application?')) return;

    try {
      // TODO: Send deallocate request through streaming context
      console.log('Deallocating application:', uid);
    } catch (error) {
      setError(`Failed to deallocate application: ${error}`);
    }
  };

  const handleGetResourceAccess = async (uid: string) => {
    try {
      // TODO: Get resource access through streaming context
      console.log('Getting resource access for:', uid);
    } catch (error) {
      setError(`Failed to get resource access: ${error}`);
    }
  };

  const filteredApps = applications.filter(app => {
    if (filterStatus !== 'all' && getStatusText(app.state) !== filterStatus) return false;
    if (filterOwner === 'mine' && !app.isUserOwned) return false;
    if (filterOwner === 'others' && app.isUserOwned) return false;
    return true;
  });

  const paginatedApps = filteredApps.slice(
    (currentPage - 1) * pageSize,
    currentPage * pageSize
  );

  const totalPages = Math.ceil(filteredApps.length / pageSize);

  const canCreateApp = hasPermission('ApplicationService', 'Create');
  const canViewAllApps = hasPermission('ApplicationService', 'GetAll');
  const canDeallocateAll = hasPermission('ApplicationService', 'DeallocateAll');

  return (
    <ProtectedRoute>
      <DashboardLayout>
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
              {/* Connection Status */}
              <div className="flex items-center space-x-2">
                <div className={`w-2 h-2 rounded-full ${
                  isConnected ? 'bg-green-500' : 'bg-red-500'
                }`} />
                <span className="text-sm text-gray-600 dark:text-gray-400">
                  {connectionStatus}
                </span>
              </div>

              {/* Create Button */}
              <button
                onClick={() => setShowCreateModal(true)}
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

          {/* Filters and Controls */}
          <div className="flex flex-wrap gap-4 items-center">
            <div className="flex items-center space-x-2">
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Sort by:
              </label>
              <select
                value={sortField}
                onChange={(e) => setSortField(e.target.value as 'name' | 'created' | 'status')}
                className="px-3 py-1 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600"
              >
                <option value="created">Created</option>
                <option value="name">Name</option>
                <option value="status">Status</option>
              </select>
              <button
                onClick={() => setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc')}
                className="px-2 py-1 text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
              >
                {sortDirection === 'asc' ? '↑' : '↓'}
              </button>
            </div>

            <div className="flex items-center space-x-2">
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Status:
              </label>
              <select
                value={filterStatus}
                onChange={(e) => setFilterStatus(e.target.value)}
                className="px-3 py-1 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600"
              >
                <option value="all">All</option>
                <option value="New">New</option>
                <option value="Elected">Elected</option>
                <option value="Allocated">Allocated</option>
                <option value="Deallocate">Deallocate</option>
                <option value="Deallocated">Deallocated</option>
                <option value="Error">Error</option>
              </select>
            </div>

            <div className="flex items-center space-x-2">
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Owner:
              </label>
              <select
                value={filterOwner}
                onChange={(e) => setFilterOwner(e.target.value)}
                className="px-3 py-1 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600"
              >
                <option value="all">All</option>
                <option value="mine">Mine</option>
                <option value="others">Others</option>
              </select>
            </div>
          </div>

          {/* Applications Table */}
          <div className="bg-white dark:bg-gray-800 shadow overflow-hidden sm:rounded-md">
            {loading ? (
              <div className="p-6 text-center">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
                <p className="mt-2 text-gray-600 dark:text-gray-400">Loading applications...</p>
              </div>
            ) : paginatedApps.length === 0 ? (
              <div className="p-6 text-center text-gray-500 dark:text-gray-400">
                No applications found
              </div>
            ) : (
              <ul className="divide-y divide-gray-200 dark:divide-gray-700">
                {paginatedApps.map((app) => (
                  <li
                    key={app.uid}
                    className="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-700 cursor-pointer"
                    onClick={() => {
                      setSelectedApp(app);
                      setShowDetailsModal(true);
                    }}
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center space-x-4">
                        <div className={`w-3 h-3 rounded-full ${getStatusColor(app.state)}`} />
                        <div>
                          <div className="flex items-center space-x-2">
                            <p className="text-sm font-medium text-gray-900 dark:text-white">
                              {app.labelUid}
                            </p>
                            {app.isUserOwned && (
                              <span className="px-2 py-1 text-xs bg-blue-100 text-blue-800 rounded-full">
                                Mine
                              </span>
                            )}
                          </div>
                          <div className="text-sm text-gray-500 dark:text-gray-400 space-x-4">
                            <span>Owner: {app.ownerName}</span>
                            <span>Status: {getStatusText(app.state)}</span>
                                                         <span>Created: {app.createdAt ? new Date(Number(app.createdAt.seconds) * 1000).toLocaleString() : 'Unknown'}</span>
                          </div>
                        </div>
                      </div>
                      <div className="flex items-center space-x-2">
                        {(app.isUserOwned || canDeallocateAll) && (
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              handleDeallocateApplication(app.uid);
                            }}
                            className="px-3 py-1 text-sm bg-red-100 text-red-800 rounded-md hover:bg-red-200"
                            title="Deallocate Application"
                          >
                            Deallocate
                          </button>
                        )}
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleGetResourceAccess(app.uid);
                          }}
                          className="px-3 py-1 text-sm bg-green-100 text-green-800 rounded-md hover:bg-green-200"
                          title="Get SSH Access"
                        >
                          SSH Access
                        </button>
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between">
              <div className="text-sm text-gray-700 dark:text-gray-300">
                Showing {((currentPage - 1) * pageSize) + 1} to {Math.min(currentPage * pageSize, filteredApps.length)} of {filteredApps.length} applications
              </div>
              <div className="flex items-center space-x-2">
                <button
                  onClick={() => setCurrentPage(Math.max(1, currentPage - 1))}
                  disabled={currentPage === 1}
                  className="px-3 py-1 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200 disabled:opacity-50"
                >
                  Previous
                </button>
                <span className="text-sm text-gray-700 dark:text-gray-300">
                  {currentPage} of {totalPages}
                </span>
                <button
                  onClick={() => setCurrentPage(Math.min(totalPages, currentPage + 1))}
                  disabled={currentPage === totalPages}
                  className="px-3 py-1 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200 disabled:opacity-50"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Create Application Modal */}
        {showCreateModal && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-2xl max-h-[90vh] overflow-y-auto">
              <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-white">
                Create Application
              </h2>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Application YAML
                  </label>
                  <textarea
                    value={createYaml}
                    onChange={(e) => setCreateYaml(e.target.value)}
                    className="w-full h-64 px-3 py-2 border border-gray-300 rounded-md font-mono text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white"
                    placeholder={`labelUid: "your-label-uid"
metadata:
  JENKINS_URL: "http://example.com"
  JENKINS_AGENT_SECRET: "secret"
  JENKINS_AGENT_NAME: "agent-name"`}
                  />
                </div>
                {yamlError && (
                  <div className="text-sm text-red-600 dark:text-red-400">
                    {yamlError}
                  </div>
                )}
                <div className="flex justify-end space-x-3">
                  <button
                    onClick={() => {
                      setShowCreateModal(false);
                      setCreateYaml('');
                      setYamlError(null);
                    }}
                    className="px-4 py-2 text-sm bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleCreateApplication}
                    className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700"
                  >
                    Create
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Application Details Modal */}
        {showDetailsModal && selectedApp && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto">
              <div className="flex justify-between items-center mb-4">
                <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                  Application Details
                </h2>
                <button
                  onClick={() => setShowDetailsModal(false)}
                  className="text-gray-500 hover:text-gray-700"
                >
                  ×
                </button>
              </div>

              <div className="space-y-6">
                {/* Basic Info */}
                <div>
                  <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                    Basic Information
                  </h3>
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">UID</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedApp.uid}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Label UID</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedApp.labelUid}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Owner</p>
                      <p className="text-sm text-gray-900 dark:text-white">{selectedApp.ownerName}</p>
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Status</p>
                      <div className="flex items-center space-x-2">
                        <div className={`w-2 h-2 rounded-full ${getStatusColor(selectedApp.state)}`} />
                        <span className="text-sm text-gray-900 dark:text-white">
                          {getStatusText(selectedApp.state)}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>

                {/* Metadata */}
                {selectedApp.metadata && (
                  <div>
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                      Metadata
                    </h3>
                    <pre className="bg-gray-100 dark:bg-gray-700 p-4 rounded-md text-sm overflow-x-auto">
                      {JSON.stringify(selectedApp.metadata, null, 2)}
                    </pre>
                  </div>
                )}

                {/* Resource Information */}
                {selectedApp.resource && (
                  <div>
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                      Resource Information
                    </h3>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Node UID</p>
                        <p className="text-sm text-gray-900 dark:text-white">{selectedApp.resource.nodeUid}</p>
                      </div>
                      <div>
                        <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Identifier</p>
                        <p className="text-sm text-gray-900 dark:text-white">{selectedApp.resource.identifier}</p>
                      </div>
                      <div>
                        <p className="text-sm font-medium text-gray-700 dark:text-gray-300">IP Address</p>
                        <p className="text-sm text-gray-900 dark:text-white">{selectedApp.resource.ipAddr}</p>
                      </div>
                      <div>
                        <p className="text-sm font-medium text-gray-700 dark:text-gray-300">HW Address</p>
                        <p className="text-sm text-gray-900 dark:text-white">{selectedApp.resource.hwAddr}</p>
                      </div>
                    </div>
                  </div>
                )}

                {/* State Details */}
                {selectedApp.state && (
                  <div>
                    <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
                      State Details
                    </h3>
                    <div className="space-y-2">
                      <div>
                        <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Description</p>
                        <p className="text-sm text-gray-900 dark:text-white">{selectedApp.state.description}</p>
                      </div>
                      <div>
                        <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Created At</p>
                                                 <p className="text-sm text-gray-900 dark:text-white">
                           {selectedApp.state.createdAt ? new Date(Number(selectedApp.state.createdAt.seconds) * 1000).toLocaleString() : 'Unknown'}
                         </p>
                      </div>
                    </div>
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
