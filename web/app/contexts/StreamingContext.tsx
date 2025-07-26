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
import { useNotification } from '../components/Notifications';
import type { ReactNode } from 'react';
import { createClient, ConnectError, Code } from '@connectrpc/connect';
import { createGrpcWebTransport } from '@connectrpc/connect-web';
import { create, fromBinary } from '@bufbuild/protobuf';
import { useAuth } from './AuthContext';
import { tokenStorage } from '../lib/auth';
import {
  StreamingService,
  StreamingServiceSubscribeRequestSchema,
  type StreamingServiceConnectRequest,
  type StreamingServiceConnectResponse,
  type StreamingServiceSubscribeResponse,
  SubscriptionType,
  ChangeType,
} from '../../gen/aquarium/v2/streaming_pb';
import {
  ApplicationService,
  ApplicationServiceListRequestSchema,
  ApplicationServiceListStateRequestSchema,
  ApplicationServiceListResourceRequestSchema,
  type Application,
  ApplicationSchema,
  type ApplicationState,
  ApplicationStateSchema,
  type ApplicationResource,
  ApplicationResourceSchema,
  type ApplicationTask,
} from '../../gen/aquarium/v2/application_pb';
import {
  LabelService,
  LabelServiceListRequestSchema,
  type Label,
  LabelSchema,
} from '../../gen/aquarium/v2/label_pb';
import {
  NodeService,
  NodeServiceListRequestSchema,
  NodeServiceGetThisRequestSchema,
  NodeServiceSetMaintenanceRequestSchema,
  type Node,
  NodeSchema,
} from '../../gen/aquarium/v2/node_pb';
import {
  UserService,
  UserServiceListRequestSchema,
  type User,
  UserSchema,
} from '../../gen/aquarium/v2/user_pb';
import {
  RoleService,
  RoleServiceListRequestSchema,
  type Role,
  RoleSchema,
} from '../../gen/aquarium/v2/role_pb';
import { GateProxySSHService } from '../../gen/aquarium/v2/gate_proxyssh_access_pb';
import {
  PermService,
  PermApplication,
  PermLabel,
  PermNode,
  PermUser,
  PermRole,
} from '../../gen/permissions/permissions_grpc';

// Transport configuration with automatic token refresh
const transport = createGrpcWebTransport({
  baseUrl: typeof window !== 'undefined' ? `${window.location.origin}/grpc` : 'http://localhost:8001/grpc',
  interceptors: [
    (next) => async (req) => {
      // Add auth header if available
      const tokens = tokenStorage.getTokens();
      if (tokens && tokens.accessToken) {
        req.header.set('authorization', `Bearer ${tokens.accessToken}`);
      }

      try {
        return await next(req);
      } catch (error: any) {
        // If we get an authentication error, try to refresh the token and retry once
        if (error?.code === 'unauthenticated' && tokens && !tokenStorage.isRefreshTokenExpired(tokens)) {
          try {
            const { authService } = await import('../lib/auth');
            const refreshResult = await authService.refreshToken(tokens.refreshToken);

            if (refreshResult.success && refreshResult.tokens) {
              tokenStorage.setTokens(refreshResult.tokens);
              // Retry the request with the new token
              req.header.set('authorization', `Bearer ${refreshResult.tokens.accessToken}`);
              return await next(req);
            }
          } catch (refreshError) {
            console.error('Token refresh failed:', refreshError);
            tokenStorage.clearTokens();
            // Redirect to login if refresh fails
            if (typeof window !== 'undefined') {
              window.location.href = '/login';
            }
          }
        }
        throw error;
      }
    },
  ],
});

// Create clients
var streamingController = new AbortController();
const streamingClient = createClient(StreamingService, transport);
const applicationClient = createClient(ApplicationService, transport);
const labelClient = createClient(LabelService, transport);
const nodeClient = createClient(NodeService, transport);
const userClient = createClient(UserService, transport);
const roleClient = createClient(RoleService, transport);
const gateProxySSHClient = createClient(GateProxySSHService, transport);

// Enhanced logging utility
const logger = {
  debug: (message: string, ...args: any[]) => {
    console.debug(`[StreamingContext] ${message}`, ...args);
  },
  info: (message: string, ...args: any[]) => {
    console.info(`[StreamingContext] ${message}`, ...args);
  },
  warn: (message: string, ...args: any[]) => {
    console.warn(`[StreamingContext] ${message}`, ...args);
  },
  error: (message: string, ...args: any[]) => {
    console.error(`[StreamingContext] ${message}`, ...args);
  },
  request: (type: string, request: any) => {
    console.debug(`[StreamingContext] REQUEST: ${type}`, request);
  },
  response: (type: string, response: any) => {
    console.debug(`[StreamingContext] RESPONSE: ${type}`, response);
  },
  subscription: (update: StreamingServiceSubscribeResponse) => {
    console.debug(`[StreamingContext] SUBSCRIPTION UPDATE: ${ChangeType[update.changeType]}, ${SubscriptionType[update.objectType]}`);
  }
};

// Data types
interface StreamingData {
  applications: Application[];
  applicationStates: Map<string, ApplicationState>;
  applicationResources: Map<string, ApplicationResource>;
  applicationTasks: Map<string, ApplicationTask[]>;
  labels: Label[];
  nodes: Node[];
  users: User[];
  roles: Role[];
}

interface DataUpdateCallback {
  (data: StreamingData): void;
}

interface StreamingContextType {
  data: StreamingData;
  isConnected: boolean;
  connectionStatus: 'connecting' | 'connected' | 'disconnected' | 'error';
  error: string | null;
  subscribe: (callback: DataUpdateCallback) => () => void;
  sendRequest: <T>(request: T, requestType: string) => Promise<any>;
  // Individual data fetching functions
  fetchApplications: () => Promise<void>;
  fetchLabels: () => Promise<void>;
  fetchNodes: () => Promise<void>;
  fetchUsers: () => Promise<void>;
  fetchRoles: () => Promise<void>;
  fetchApplicationStates: () => Promise<void>;
  fetchApplicationResources: () => Promise<void>;
  // Node service functions
  getThisNode: () => Promise<Node | null>;
  setNodeMaintenance: (maintenance?: boolean, shutdown?: boolean, shutdownDelay?: string) => Promise<boolean>;
  // Utility functions
  resetFetchedDataTypes: () => void;
}

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
  const { user, isAuthenticated, hasPermission } = useAuth();
  const initialData: StreamingData = {
    applications: [],
    applicationStates: new Map(),
    applicationResources: new Map(),
    applicationTasks: new Map(),
    labels: [],
    nodes: [],
    users: [],
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
    var args: [string, string?] = [message]
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

  // Refs for managing streams
  const connectStreamRef = useRef<AsyncIterable<StreamingServiceConnectResponse> | null>(null);
  const subscribeStreamRef = useRef<AsyncIterable<StreamingServiceSubscribeResponse> | null>(null);
  const callbacksRef = useRef<Set<DataUpdateCallback>>(new Set());
  const currentDataRef = useRef<StreamingData>(initialData);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const isReconnectingRef = useRef(false);
  const pendingRequestsRef = useRef<Map<string, { resolve: (value: any) => void; reject: (reason?: any) => void }>>(new Map());
  const requestQueueRef = useRef<StreamingServiceConnectRequest[]>([]);
  const connectWriterRef = useRef<WritableStream<StreamingServiceConnectRequest> | null>(null);

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

  // Individual data fetching functions
  const fetchApplications = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if applications have already been fetched in this session
    if (fetchedDataTypes.has('applications')) {
      logger.debug('Applications already fetched in this session, skipping');
      return;
    }

    const canListApplications = hasPermission(PermService.Application, PermApplication.List);
    if (!canListApplications) {
      logger.info('User does not have permission to list applications');
      return;
    }

    try {
      logger.info('Fetching applications with status & resource...');
      const response = await applicationClient.list(create(ApplicationServiceListRequestSchema));
      const applications = response.data || [];

      setData(prevData => {
        const newData = { ...prevData, applications };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // If we have applications, fetch their states and resources
      if (applications.length > 0) {
        await Promise.all([
          fetchApplicationStates(),
          fetchApplicationResources()
        ]);
      }

      // Mark applications as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'applications']));
      logger.info('Applications fetched:', applications.length);
    } catch (err) {
      logger.error('Failed to fetch applications:', err);
      addNotification('error', 'Failed to fetch applications', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  const fetchLabels = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if labels have already been fetched in this session
    if (fetchedDataTypes.has('labels')) {
      logger.debug('Labels already fetched in this session, skipping');
      return;
    }

    const canListLabels = hasPermission(PermService.Label, PermLabel.List);
    if (!canListLabels) {
      logger.info('User does not have permission to list labels');
      return;
    }

    try {
      logger.info('Fetching labels...');
      const response = await labelClient.list(create(LabelServiceListRequestSchema));
      const labels = response.data || [];

      setData(prevData => {
        const newData = { ...prevData, labels };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // Mark labels as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'labels']));
      logger.info('Labels fetched:', labels.length);
    } catch (err) {
      logger.error('Failed to fetch labels:', err);
      addNotification('error', 'Failed to fetch labels', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  const fetchNodes = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if nodes have already been fetched in this session
    if (fetchedDataTypes.has('nodes')) {
      logger.debug('Nodes already fetched in this session, skipping');
      return;
    }

    const canListNodes = hasPermission(PermService.Node, PermNode.List);
    if (!canListNodes) {
      logger.info('User does not have permission to list nodes');
      return;
    }

    try {
      logger.info('Fetching nodes...');
      const response = await nodeClient.list(create(NodeServiceListRequestSchema));
      const nodes = response.data || [];

      setData(prevData => {
        const newData = { ...prevData, nodes };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // Mark nodes as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'nodes']));
      logger.info('Nodes fetched:', nodes.length);
    } catch (err) {
      logger.error('Failed to fetch nodes:', err);
      addNotification('error', 'Failed to fetch nodes', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  const fetchUsers = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if users have already been fetched in this session
    if (fetchedDataTypes.has('users')) {
      logger.debug('Users already fetched in this session, skipping');
      return;
    }

    const canListUsers = hasPermission(PermService.User, PermUser.List);
    if (!canListUsers) {
      logger.info('User does not have permission to list users');
      return;
    }

    try {
      logger.info('Fetching users...');
      const response = await userClient.list(create(UserServiceListRequestSchema));
      const users = response.data || [];

      setData(prevData => {
        const newData = { ...prevData, users };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // Mark users as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'users']));
      logger.info('Users fetched:', users.length);
    } catch (err) {
      logger.error('Failed to fetch users:', err);
      addNotification('error', 'Failed to fetch users', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  const fetchRoles = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if roles have already been fetched in this session
    if (fetchedDataTypes.has('roles')) {
      logger.debug('Roles already fetched in this session, skipping');
      return;
    }

    const canListRoles = hasPermission(PermService.Role, PermRole.List);
    if (!canListRoles) {
      logger.info('User does not have permission to list roles');
      return;
    }

    try {
      logger.info('Fetching roles...');
      const response = await roleClient.list(create(RoleServiceListRequestSchema));
      const roles = response.data || [];

      setData(prevData => {
        const newData = { ...prevData, roles };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // Mark roles as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'roles']));
      logger.info('Roles fetched:', roles.length);
    } catch (err) {
      logger.error('Failed to fetch roles:', err);
      addNotification('error', 'Failed to fetch roles', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  const fetchApplicationStates = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if application states have already been fetched in this session
    if (fetchedDataTypes.has('applicationStates')) {
      logger.debug('ApplicationStates already fetched in this session, skipping');
      return;
    }

    const canListApplicationState = hasPermission(PermService.Application, PermApplication.ListState);
    if (!canListApplicationState) {
      logger.info('User does not have permission to list application states');
      return;
    }

    try {
      logger.info('Fetching all application states...');

      const response = await applicationClient.listState(create(ApplicationServiceListStateRequestSchema));
      const states = response.data || [];
      const newStates = new Map<string, ApplicationState>();

      states.forEach(state => {
        if (state.applicationUid) {
          newStates.set(state.applicationUid, state);
        }
      });

      setData(prevData => {
        const newData = {
          ...prevData,
          applicationStates: new Map([...prevData.applicationStates, ...newStates])
        };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // Mark application states as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'applicationStates']));
      logger.info('Application states fetched:', newStates.size);
    } catch (err) {
      logger.error('Failed to fetch application states:', err);
      addNotification('error', 'Failed to fetch application states', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  const fetchApplicationResources = useCallback(async () => {
    if (!isAuthenticated) return;

    // Check if application resources have already been fetched in this session
    if (fetchedDataTypes.has('applicationResources')) {
      logger.debug('ApplicationResources already fetched in this session, skipping');
      return;
    }

    const canListApplicationResource = hasPermission(PermService.Application, PermApplication.ListResource);
    if (!canListApplicationResource) {
      logger.info('User does not have permission to list application resources');
      return;
    }

    try {
      logger.info('Fetching all application resources...');

      const response = await applicationClient.listResource(create(ApplicationServiceListResourceRequestSchema));
      const resources = response.data || [];
      const newResources = new Map<string, ApplicationResource>();

      resources.forEach(resource => {
        if (resource.applicationUid) {
          newResources.set(resource.applicationUid, resource);
        }
      });

      setData(prevData => {
        const newData = {
          ...prevData,
          applicationResources: new Map([...prevData.applicationResources, ...newResources])
        };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      // Mark application resources as fetched
      setFetchedDataTypes(prev => new Set([...prev, 'applicationResources']));
      logger.info('Application resources fetched:', newResources.size);
    } catch (err) {
      logger.error('Failed to fetch application resources:', err);
      addNotification('error', 'Failed to fetch application resources', String(err));
    }
  }, [isAuthenticated, hasPermission, notifySubscribers, addNotification, fetchedDataTypes]);

  // Node service functions
  const getThisNode = useCallback(async (): Promise<Node | null> => {
    if (!isAuthenticated) return null;

    const canGetThisNode = hasPermission(PermService.Node, PermNode.GetThis);
    if (!canGetThisNode) {
      logger.info('User does not have permission to get this node');
      return null;
    }

    try {
      logger.info('Fetching current node...');
      const response = await nodeClient.getThis(create(NodeServiceGetThisRequestSchema));
      const node = response.data || null;
      logger.info('Current node fetched:', node?.name);
      return node;
    } catch (err) {
      logger.error('Failed to fetch current node:', err);
      addNotification('error', 'Failed to fetch current node', String(err));
      return null;
    }
  }, [isAuthenticated, hasPermission, addNotification]);

  const setNodeMaintenance = useCallback(async (maintenance?: boolean, shutdown?: boolean, shutdownDelay?: string): Promise<boolean> => {
    if (!isAuthenticated) return false;

    const canSetMaintenance = hasPermission(PermService.Node, PermNode.SetMaintenance);
    if (!canSetMaintenance) {
      logger.info('User does not have permission to set node maintenance');
      addNotification('error', 'Permission denied', 'You do not have permission to set node maintenance');
      return false;
    }

    try {
      logger.info('Setting node maintenance:', { maintenance, shutdown, shutdownDelay });
      const response = await nodeClient.setMaintenance(create(NodeServiceSetMaintenanceRequestSchema, {
        maintenance,
        shutdown,
        shutdownDelay,
      }));
      const success = response.status;

      if (success) {
        const action = shutdown ? 'shutdown' : (maintenance ? 'maintenance mode enabled' : 'maintenance mode disabled');
        addNotification('info', `Node ${action} successfully`);
      } else {
        addNotification('error', 'Failed to set node maintenance', 'The operation was not successful');
      }

      logger.info('Node maintenance set:', success);
      return success;
    } catch (err) {
      logger.error('Failed to set node maintenance:', err);
      addNotification('error', 'Failed to set node maintenance', String(err));
      return false;
    }
  }, [isAuthenticated, hasPermission, addNotification]);

  // Utility function to reset fetched data types (useful for manual refresh)
  const resetFetchedDataTypes = useCallback(() => {
    logger.info('Resetting fetched data types');
    setFetchedDataTypes(new Set());
  }, []);

  // Handle subscription updates
  const handleSubscriptionUpdate = useCallback((response: StreamingServiceSubscribeResponse) => {
    logger.subscription(response);

    setData(prevData => {
      const newData = { ...prevData };

      try {
        const binaryData = response.objectData?.value;
        if (!binaryData || !(binaryData instanceof Uint8Array)) {
          logger.warn('No binary data or invalid data type in subscription update');
          return prevData;
        }

        switch (response.objectType) {
          case SubscriptionType.APPLICATION: {
            const app = fromBinary(ApplicationSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.applications = newData.applications.filter(a => a.uid !== app.uid);
                newData.applications.push(app);
                logger.info(`Application ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, app.uid);
                break;
              case ChangeType.REMOVED:
                newData.applications = newData.applications.filter(a => a.uid !== app.uid);
                logger.info('Application removed:', app.uid);
                break;
            }
            break;
          }

          case SubscriptionType.APPLICATION_STATE: {
            const state = fromBinary(ApplicationStateSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.applicationStates.set(state.applicationUid, state);
                // Force applications array update to trigger UI recomputation
                newData.applications = [...newData.applications];
                logger.info(`Application state ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'} for:`, state.applicationUid, 'Status:', state.status);
                break;
              case ChangeType.REMOVED:
                newData.applicationStates.delete(state.applicationUid);
                newData.applications = [...newData.applications];
                logger.info('Application state removed for:', state.applicationUid);
                break;
            }
            break;
          }

          case SubscriptionType.APPLICATION_RESOURCE: {
            const resource = fromBinary(ApplicationResourceSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.applicationResources.set(resource.applicationUid, resource);
                // Force applications array update to trigger UI recomputation
                newData.applications = [...newData.applications];
                logger.info(`Application resource ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'} for:`, resource.applicationUid);
                break;
              case ChangeType.REMOVED:
                newData.applicationResources.delete(resource.applicationUid);
                newData.applications = [...newData.applications];
                logger.info('Application resource removed for:', resource.applicationUid);
                break;
            }
            break;
          }

          case SubscriptionType.LABEL: {
            const label = fromBinary(LabelSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.labels = newData.labels.filter(l => l.uid !== label.uid);
                newData.labels.push(label);
                logger.info(`Label ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, label.name);
                break;
              case ChangeType.REMOVED:
                newData.labels = newData.labels.filter(l => l.uid !== label.uid);
                logger.info('Label removed:', label.name);
                break;
            }
            break;
          }

          case SubscriptionType.NODE: {
            const node = fromBinary(NodeSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.nodes = newData.nodes.filter(n => n.uid !== node.uid);
                newData.nodes.push(node);
                logger.info(`Node ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, node.name);
                break;
              case ChangeType.REMOVED:
                newData.nodes = newData.nodes.filter(n => n.uid !== node.uid);
                logger.info('Node removed:', node.name);
                break;
            }
            break;
          }

          case SubscriptionType.USER: {
            const user = fromBinary(UserSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.users = newData.users.filter(u => u.name !== user.name);
                newData.users.push(user);
                logger.info(`User ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, user.name);
                break;
              case ChangeType.REMOVED:
                newData.users = newData.users.filter(u => u.name !== user.name);
                logger.info('User removed:', user.name);
                break;
            }
            break;
          }

          case SubscriptionType.ROLE: {
            const role = fromBinary(RoleSchema, binaryData);
            switch (response.changeType) {
              case ChangeType.CREATED:
              case ChangeType.UPDATED:
                newData.roles = newData.roles.filter(r => r.name !== role.name);
                newData.roles.push(role);
                logger.info(`Role ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, role.name);
                break;
              case ChangeType.REMOVED:
                newData.roles = newData.roles.filter(r => r.name !== role.name);
                logger.info('Role removed:', role.name);
                break;
            }
            break;
          }
        }
      } catch (err) {
        logger.error('Error processing subscription update:', err);
        addNotification('error', 'Error processing subscription update', String(err));
      }

      currentDataRef.current = newData;
      notifySubscribers(newData);
      return newData;
    });
  }, [notifySubscribers, addNotification]);

  // Handle Connect stream responses
  const handleConnectResponse = useCallback((response: StreamingServiceConnectResponse) => {
    const { requestId, error: streamError, responseData } = response;
    logger.response('ConnectResponse', { requestId, error: streamError, hasData: !!responseData });

    const pendingRequest = pendingRequestsRef.current.get(requestId);
    if (pendingRequest) {
      pendingRequestsRef.current.delete(requestId);

      if (streamError) {
        const errorMsg = `Stream error: ${streamError.message}`;
        logger.error('Request failed:', errorMsg);
        addNotification('error', 'Request failed', errorMsg);
        pendingRequest.reject(new Error(errorMsg));
      } else {
        logger.info('Request completed successfully for:', requestId);
        pendingRequest.resolve(responseData);
      }
    }
  }, [addNotification]);

  // Enhanced sendRequest with bidirectional streaming support
  const sendRequest = useCallback(async (request: any, requestType: string): Promise<any> => {
    logger.request(requestType, request);

    try {
      let response;
      switch (requestType) {
        case 'ApplicationServiceCreateRequest':
          response = await applicationClient.create(request);
          addNotification('info', 'Application created successfully');
          break;
        case 'ApplicationServiceDeallocateRequest':
          response = await applicationClient.deallocate(request);
          addNotification('info', 'Application deallocated successfully');
          break;
        case 'GateProxySSHServiceGetResourceAccessRequest':
          response = await gateProxySSHClient.getResourceAccess(request);
          addNotification('info', 'Resource access information retrieved');
          break;
        case 'LabelServiceCreateRequest':
          response = await labelClient.create(request);
          addNotification('info', 'Label created successfully');
          break;
        case 'LabelServiceRemoveRequest':
          response = await labelClient.remove(request);
          addNotification('info', 'Label deleted successfully');
          break;
        case 'UserServiceCreateRequest':
          response = await userClient.create(request);
          addNotification('info', 'User created successfully');
          break;
        case 'UserServiceUpdateRequest':
          response = await userClient.update(request);
          addNotification('info', 'User updated successfully');
          break;
        case 'UserServiceRemoveRequest':
          response = await userClient.remove(request);
          addNotification('info', 'User deleted successfully');
          break;
        case 'RoleServiceCreateRequest':
          response = await roleClient.create(request);
          addNotification('info', 'Role created successfully');
          break;
        case 'RoleServiceUpdateRequest':
          response = await roleClient.update(request);
          addNotification('info', 'Role updated successfully');
          break;
        case 'RoleServiceRemoveRequest':
          response = await roleClient.remove(request);
          addNotification('info', 'Role deleted successfully');
          break;
        default:
          throw new Error(`Unsupported request type: ${requestType}`);
      }

      logger.response(requestType, response);
      return response;

    } catch (err) {
      const errorMsg = `${requestType} failed: ${err}`;
      logger.error('Request failed:', err);
      addNotification('error', `Operation failed`, errorMsg);
      throw err;
    }
  }, [addNotification]);

  // Connect to streaming
  const connect = useCallback(async () => {
    if (isReconnectingRef.current || !isAuthenticated) return;

    try {
      logger.info('Connecting to streaming service...');
      setConnectionStatus('connecting');
      setError(null);

      // Create subscription request
      const subscriptionTypes: SubscriptionType[] = [];
      hasPermission(PermService.Application, PermApplication.Get) && subscriptionTypes.push(SubscriptionType.APPLICATION);
      hasPermission(PermService.Application, PermApplication.GetState) && subscriptionTypes.push(SubscriptionType.APPLICATION_STATE);
      hasPermission(PermService.Application, PermApplication.GetResource) && subscriptionTypes.push(SubscriptionType.APPLICATION_RESOURCE);
      hasPermission(PermService.Application, PermApplication.GetTask) && subscriptionTypes.push(SubscriptionType.APPLICATION_TASK);
      hasPermission(PermService.Label, PermLabel.Get) && subscriptionTypes.push(SubscriptionType.LABEL);
      hasPermission(PermService.Node, PermNode.Get) && subscriptionTypes.push(SubscriptionType.NODE);
      hasPermission(PermService.User, PermUser.Get) && subscriptionTypes.push(SubscriptionType.USER);
      hasPermission(PermService.Role, PermRole.Get) && subscriptionTypes.push(SubscriptionType.ROLE);

      const subscribeRequest = create(StreamingServiceSubscribeRequestSchema, {
        subscriptionTypes: subscriptionTypes,
      });

      logger.debug('Subscribing with request:', subscribeRequest);

      // Create subscription stream with abort signal
      subscribeStreamRef.current = streamingClient.subscribe(subscribeRequest, { signal: streamingController.signal });

      setConnectionStatus('connected');
      setIsConnected(true);
      logger.info('Successfully connected to streaming service');
      addNotification('info', 'Connected to real-time updates');

      // Process subscription updates
      for await (const response of subscribeStreamRef.current) {
        handleSubscriptionUpdate(response);
      }
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Canceled) {
        // This is a valid abort due to logout - no need to panic
        return
      }
      logger.error('Streaming connection error:', err);
      const errorMsg = `Connection error: ${err}`;
      setError(errorMsg);
      addNotification('error', 'Connection failed', errorMsg);
      setConnectionStatus('error');
      setIsConnected(false);

      // Schedule reconnection
      if (!isReconnectingRef.current) {
        isReconnectingRef.current = true;
        logger.info('Scheduling reconnection in 10 seconds...');
        reconnectTimeoutRef.current = window.setTimeout(() => {
          isReconnectingRef.current = false;
          connect();
        }, 10000);
      }
    }
  }, [isAuthenticated, handleSubscriptionUpdate, addNotification]);

  // Disconnect from streaming
  const disconnect = useCallback(() => {
    const wasConnected = subscribeStreamRef.current !== null

    logger.info('Disconnecting from streaming service...');

    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    connectStreamRef.current = null;
    subscribeStreamRef.current = null;
    connectWriterRef.current = null;
    isReconnectingRef.current = false;

    // Reject all pending requests
    pendingRequestsRef.current.forEach(({ reject }) => {
      reject(new Error('Connection closed'));
    });
    pendingRequestsRef.current.clear();
    requestQueueRef.current = [];

    if (wasConnected) {
      // Disconnect from stream
      streamingController.abort("Disconnected by client");
      // Updating controller to allow the new user to login again
      streamingController = new AbortController();

      setIsConnected(false);
      setConnectionStatus('disconnected');
      addNotification('info', 'Disconnected from streaming service');
    }

    // Reset fetched data types when disconnecting (new session will need fresh data)
    setFetchedDataTypes(new Set());

  }, [addNotification]);

  // Effect to manage connection
  useEffect(() => {
    if (isAuthenticated && user) {
      logger.info('User authenticated, initializing streaming...');
      connect();
    } else {
      logger.info('User not authenticated, disconnecting...');
      disconnect();
    }

    return () => {
      disconnect();
    };
  }, [isAuthenticated, user, connect, disconnect]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      disconnect();
    };
  }, [disconnect]);

  const contextValue: StreamingContextType = {
    data,
    isConnected,
    connectionStatus,
    error,
    subscribe,
    sendRequest,
    fetchApplications,
    fetchLabels,
    fetchNodes,
    fetchUsers,
    fetchRoles,
    fetchApplicationStates,
    fetchApplicationResources,
    getThisNode,
    setNodeMaintenance,
    resetFetchedDataTypes,
  };

  return (
    <StreamingContext.Provider value={contextValue}>
      {children}
    </StreamingContext.Provider>
  );
};
