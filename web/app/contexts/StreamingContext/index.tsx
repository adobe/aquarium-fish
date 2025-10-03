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

import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { useNotification } from '../../components/Notifications';
import type { ReactNode } from 'react';
import { useAuth } from '../AuthContext';
import { SubscriptionType } from '../../../gen/aquarium/v2/streaming_pb';
import { PermService, PermApplication, PermLabel, PermNode, PermUser, PermRole } from '../../../gen/permissions/permissions_grpc';
import { StreamingConnection, logger } from './connection';
import { handleSubscriptionUpdate } from './subscriptions';
import { ApplicationsProvider } from './providers/ApplicationsProvider';
import { EntityProvider } from './providers/EntityProvider';
import { labelsService } from '../../features/labels/api/labels.service';
import { nodesService } from '../../features/nodes/api/nodes.service';
import { usersService } from '../../features/users/api/users.service';
import { rolesService } from '../../features/roles/api/roles.service';
import { usergroupsService } from '../../features/usergroups/api/usergroups.service';
import type { StreamingData, DataUpdateCallback, StreamingContextType } from './types';

const StreamingContext = createContext<StreamingContextType | undefined>(undefined);

export const useStreaming = () => {
  const context = useContext(StreamingContext);
  if (context === undefined) {
    throw new Error('useStreaming must be used within a StreamingProvider');
  }
  return context;
};

interface StreamingProviderProps {
  children: ReactNode;
}

export const StreamingProvider: React.FC<StreamingProviderProps> = ({ children }) => {
  const { user, isAuthenticated, hasPermission, logout } = useAuth();
  const initialData: StreamingData = {
    applications: [],
    applicationStates: new Map(),
    applicationResources: new Map(),
    applicationTasks: new Map(),
    labels: [],
    nodes: [],
    users: [],
    usergroups: [],
    roles: [],
  };

  const [data, setData] = useState<StreamingData>(initialData);
  const [isConnected, setIsConnected] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('disconnected');
  const [error, setError] = useState<string | null>(null);
  // Track which data types have been fetched in this session
  const [fetchedDataTypes, setFetchedDataTypes] = useState<Set<string>>(new Set());

  // Use notification context
  const { sendNotification } = useNotification();
  const addNotification = useCallback((type: 'error' | 'warning' | 'info', message: string, details?: string) => {
    sendNotification(type, message, details);
    // Log to console as well
    const args: [string, string?] = [message];
    if (details !== undefined) {
      args.push(details);
    }
    switch (type) {
      case 'error':
        logger.error(...args);
        break;
      case 'warning':
        logger.warn(...args);
        break;
      case 'info':
        logger.info(...args);
        break;
    }
  }, [sendNotification]);

  // Refs for managing data and subscriptions
  const callbacksRef = useRef<Set<DataUpdateCallback>>(new Set());
  const currentDataRef = useRef<StreamingData>(initialData);
  const connectionRef = useRef<StreamingConnection>(new StreamingConnection());
  const fetchedDataTypesRef = useRef<Set<string>>(new Set());

  // Update ref when fetchedDataTypes changes
  useEffect(() => {
    fetchedDataTypesRef.current = fetchedDataTypes;
  }, [fetchedDataTypes]);

  // Subscribe to data updates
  const subscribe = useCallback((callback: DataUpdateCallback) => {
    callbacksRef.current.add(callback);
    // Immediately call with current data from ref
    callback(currentDataRef.current);

    return () => {
      callbacksRef.current.delete(callback);
    };
  }, []);

  // Notify all subscribers
  const notifySubscribers = useCallback((newData: StreamingData) => {
    callbacksRef.current.forEach(callback => callback(newData));
  }, []);

  // Create providers
  const applicationsProvider = useRef(new ApplicationsProvider());
  const labelsProvider = useRef(new EntityProvider('labels', 'labels', labelsService.list.bind(labelsService), { service: PermService.Label, action: PermLabel.List }));
  const nodesProvider = useRef(new EntityProvider('nodes', 'nodes', nodesService.list.bind(nodesService), { service: PermService.Node, action: PermNode.List }));
  const usersProvider = useRef(new EntityProvider('users', 'users', usersService.list.bind(usersService), { service: PermService.User, action: PermUser.List }));
  const rolesProvider = useRef(new EntityProvider('roles', 'roles', rolesService.list.bind(rolesService), { service: PermService.Role, action: PermRole.List }));
  const usergroupsProvider = useRef(new EntityProvider('usergroups', 'usergroups', usergroupsService.list.bind(usergroupsService), { service: PermService.User, action: PermUser.List }));

  // Individual data fetching functions
  const fetchApplications = useCallback(async () => {
    if (!isAuthenticated) return;
    await applicationsProvider.current.fetchApplications(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    // Also fetch states and resources if we have applications
    if (currentDataRef.current.applications.length > 0) {
      await Promise.all([
        applicationsProvider.current.fetchApplicationStates(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification),
        applicationsProvider.current.fetchApplicationResources(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification)
      ]);
    }
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchLabels = useCallback(async () => {
    if (!isAuthenticated) return;
    await labelsProvider.current.fetch(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchNodes = useCallback(async () => {
    if (!isAuthenticated) return;
    await nodesProvider.current.fetch(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchUsers = useCallback(async () => {
    if (!isAuthenticated) return;
    await usersProvider.current.fetch(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchRoles = useCallback(async () => {
    if (!isAuthenticated) return;
    await rolesProvider.current.fetch(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchUserGroups = useCallback(async () => {
    if (!isAuthenticated) return;
    await usergroupsProvider.current.fetch(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchApplicationStates = useCallback(async () => {
    if (!isAuthenticated) return;
    await applicationsProvider.current.fetchApplicationStates(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  const fetchApplicationResources = useCallback(async () => {
    if (!isAuthenticated) return;
    await applicationsProvider.current.fetchApplicationResources(fetchedDataTypesRef.current, setData, notifySubscribers, currentDataRef, hasPermission, addNotification);
    setFetchedDataTypes(new Set(fetchedDataTypesRef.current));
  }, [isAuthenticated, notifySubscribers, hasPermission, addNotification]);

  // Utility function to reset fetched data types (useful for manual refresh)
  const resetFetchedDataTypes = useCallback(() => {
    logger.info('Resetting fetched data types');
    fetchedDataTypesRef.current.clear();
    setFetchedDataTypes(new Set());
  }, []);

  // Clear all dashboard data
  const clearData = useCallback(() => {
    logger.info('Clearing all dashboard data');
    const emptyData: StreamingData = {
      applications: [],
      applicationStates: new Map(),
      applicationResources: new Map(),
      applicationTasks: new Map(),
      labels: [],
      nodes: [],
      users: [],
      usergroups: [],
      roles: [],
    };
    setData(emptyData);
    currentDataRef.current = emptyData;
    notifySubscribers(emptyData);
    // Also reset fetched data types
    fetchedDataTypesRef.current.clear();
    setFetchedDataTypes(new Set());
  }, [notifySubscribers]);

  // Handle subscription updates
  const handleUpdate = useCallback((response: any) => {
    setData(prevData => {
      const newData = handleSubscriptionUpdate(response, prevData);
      currentDataRef.current = newData;
      notifySubscribers(newData);
      return newData;
    });
  }, [notifySubscribers]);

  // Connect to streaming
  const connect = useCallback(async () => {
    if (!isAuthenticated) return;

    // Don't reconnect if already connected
    if (connectionRef.current.isConnectedState()) {
      logger.debug('Already connected to streaming service');
      return;
    }

    setConnectionStatus('connecting');
    setError(null);

    // Create subscription request
    const subscriptionTypes: SubscriptionType[] = [];
    if (hasPermission(PermService.Application, PermApplication.Get)) subscriptionTypes.push(SubscriptionType.APPLICATION);
    if (hasPermission(PermService.Application, PermApplication.GetState)) subscriptionTypes.push(SubscriptionType.APPLICATION_STATE);
    if (hasPermission(PermService.Application, PermApplication.GetResource)) subscriptionTypes.push(SubscriptionType.APPLICATION_RESOURCE);
    if (hasPermission(PermService.Application, PermApplication.GetTask)) subscriptionTypes.push(SubscriptionType.APPLICATION_TASK);
    if (hasPermission(PermService.Label, PermLabel.Get)) subscriptionTypes.push(SubscriptionType.LABEL);
    if (hasPermission(PermService.Node, PermNode.Get)) subscriptionTypes.push(SubscriptionType.NODE);
    if (hasPermission(PermService.User, PermUser.Get)) subscriptionTypes.push(SubscriptionType.USER);
    if (hasPermission(PermService.User, PermUser.Get)) subscriptionTypes.push(SubscriptionType.USER_GROUP);
    if (hasPermission(PermService.Role, PermRole.Get)) subscriptionTypes.push(SubscriptionType.ROLE);

    await connectionRef.current.connect(
      subscriptionTypes,
      handleUpdate,
      (errorMsg: string) => {
        setError(errorMsg);
        addNotification('error', 'Connection failed', errorMsg);
        setConnectionStatus('error');
        setIsConnected(false);
      },
      () => {
        setConnectionStatus('connected');
        setIsConnected(true);
        addNotification('info', 'Connected to real-time updates');
      },
      () => {
        setIsConnected(false);
        setConnectionStatus('disconnected');
      },
      () => {
        // Handle 401 Unauthenticated - logout user
        logger.warn('Streaming connection received 401, triggering logout');
        addNotification('warning', 'Session expired', 'You have been logged out due to authentication failure');
        logout();
      }
    );
  }, [isAuthenticated, hasPermission, handleUpdate, addNotification, logout]);

  // Disconnect from streaming
  const disconnect = useCallback(() => {
    const wasConnected = connectionRef.current.isConnectedState();
    connectionRef.current.disconnect();

    if (wasConnected) {
      setIsConnected(false);
      setConnectionStatus('disconnected');
      addNotification('info', 'Disconnected from streaming service');
    }

    // Clear all data and reset fetched data types when disconnecting (new session will need fresh data)
    clearData();
  }, [addNotification, clearData]);

  // Effect to manage connection - only runs when auth state changes
  useEffect(() => {
    if (isAuthenticated && user) {
      logger.info('User authenticated, initializing streaming...');
      connect();
    } else {
      logger.info('User not authenticated, disconnecting...');
      disconnect();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAuthenticated, user]); // Only depend on auth state, not connect/disconnect functions

  const contextValue: StreamingContextType = {
    data,
    isConnected,
    connectionStatus,
    error,
    subscribe,
    fetchApplications,
    fetchLabels,
    fetchNodes,
    fetchUsers,
    fetchUserGroups,
    fetchRoles,
    fetchApplicationStates,
    fetchApplicationResources,
    resetFetchedDataTypes,
    clearData,
  };

  return (
    <StreamingContext.Provider value={contextValue}>
      {children}
    </StreamingContext.Provider>
  );
};

// Re-export for backward compatibility
export type { StreamingData, StreamingContextType };

