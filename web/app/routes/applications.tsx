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
import { ApplicationServiceDeallocateRequestSchema, ApplicationServiceGetResourceRequestSchema } from '../../gen/aquarium/v2/application_pb';
import { ApplicationForm } from '../../gen/components';

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
  const { data, isConnected, connectionStatus, sendRequest } = useStreaming();
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

  const handleCreateApplication = async (applicationData: Application) => {
    try {
      console.log('Creating application:', applicationData);

      const application = create(ApplicationServiceCreateRequestSchema, {
        application: {
          ...applicationData,
          uid: crypto.randomUUID(),
          ownerName: user?.userName || '',
        },
      });
      await sendRequest(application, 'ApplicationServiceCreateRequest');

      setShowCreateModal(false);
    } catch (error) {
      setError(`Failed to create application: ${error}`);
    }
  };

  const handleDeallocateApplication = async (uid: string) => {
    if (!confirm('Are you sure you want to deallocate this application?')) return;

    try {
      const deallocateRequest = create(ApplicationServiceDeallocateRequestSchema, {
        applicationUid: uid,
      });
      await sendRequest(deallocateRequest, 'ApplicationServiceDeallocateRequest');
      console.log('Application deallocated:', uid);
    } catch (error) {
      setError(`Failed to deallocate application: ${error}`);
    }
  };

  const handleGetResourceAccess = async (uid: string) => {
    try {
      const resourceRequest = create(ApplicationServiceGetResourceRequestSchema, {
        applicationUid: uid,
      });
      const response = await sendRequest(resourceRequest, 'ApplicationServiceGetResourceRequest');
      console.log('Resource access info:', response);
      // TODO: Display resource access information or SSH connection details
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
              <ApplicationForm
                mode="view"
                initialData={selectedApp}
                onSubmit={() => {}} // Not used in view mode
                onCancel={() => setShowDetailsModal(false)}
                title="Application Details"
              />
            </div>
          </div>
        )}
      </DashboardLayout>
    </ProtectedRoute>
  );
}
