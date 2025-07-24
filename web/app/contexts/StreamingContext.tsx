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
import { createClient } from '@connectrpc/connect';
import { createGrpcWebTransport } from '@connectrpc/connect-web';
import { create, fromBinary } from '@bufbuild/protobuf';
import { useAuth } from './AuthContext';
import { tokenStorage } from '../lib/auth';
import {
  StreamingService,
  StreamingServiceConnectRequestSchema,
  StreamingServiceSubscribeRequestSchema,
  type StreamingServiceConnectRequest,
  type StreamingServiceConnectResponse,
  type StreamingServiceSubscribeRequest,
  type StreamingServiceSubscribeResponse,
  SubscriptionType,
  ChangeType,
} from '../../gen/aquarium/v2/streaming_pb';
import {
  ApplicationService,
  ApplicationServiceListRequestSchema,
  ApplicationServiceGetStateRequestSchema,
  ApplicationServiceGetResourceRequestSchema,
  type ApplicationServiceListResponse,
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
  type LabelServiceListResponse,
  type Label,
  LabelSchema,
} from '../../gen/aquarium/v2/label_pb';
import {
  NodeService,
  NodeServiceListRequestSchema,
  type NodeServiceListResponse,
  type Node,
  NodeSchema,
} from '../../gen/aquarium/v2/node_pb';
import {
  UserService,
  UserServiceListRequestSchema,
  type UserServiceListResponse,
  type User,
  UserSchema,
} from '../../gen/aquarium/v2/user_pb';
import {
  RoleService,
  RoleServiceListRequestSchema,
  type RoleServiceListResponse,
  type Role,
  RoleSchema,
} from '../../gen/aquarium/v2/role_pb';
import { GateProxySSHService } from '../../gen/aquarium/v2/gate_proxyssh_access_pb';

// Transport configuration with automatic token refresh
const transport = createGrpcWebTransport({
  baseUrl: typeof window !== 'undefined' ? `${window.location.origin}/grpc` : 'http://localhost:8001/grpc',
  interceptors: [
    (next) => async (req) => {
      // Add auth header if available
      let tokens = tokenStorage.getTokens();
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
    console.group(`[StreamingContext] REQUEST: ${type}`);
    console.debug('Request data:', request);
    console.groupEnd();
  },
  response: (type: string, response: any) => {
    console.group(`[StreamingContext] RESPONSE: ${type}`);
    console.debug('Response data:', response);
    console.groupEnd();
  },
  subscription: (update: StreamingServiceSubscribeResponse) => {
    console.group(`[StreamingContext] SUBSCRIPTION UPDATE`);
    console.debug('Subscription type:', update.objectType);
    console.debug('Change type:', update.changeType);
    console.debug('Object type:', update.objectData?.typeUrl || 'No object data');
    console.groupEnd();
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
  refreshData: () => Promise<void>;
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
  const { user, isAuthenticated } = useAuth();
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

  // Initial data fetch using streaming where possible
  const refreshData = useCallback(async () => {
    if (!isAuthenticated) return;

    logger.info('Refreshing data...');

    try {
      // Use direct calls for initial data load - this ensures we have data immediately
      // The streaming will update this data in real-time
      const [appsRes, labelsRes, nodesRes, usersRes, rolesRes] = await Promise.all([
        applicationClient.list(create(ApplicationServiceListRequestSchema)),
        labelClient.list(create(LabelServiceListRequestSchema)),
        nodeClient.list(create(NodeServiceListRequestSchema)),
        userClient.list(create(UserServiceListRequestSchema)),
        roleClient.list(create(RoleServiceListRequestSchema)),
      ]);

      const applications = appsRes.data || [];

      // Fetch all states and resources in parallel
      const statePromises = applications.map(app =>
        applicationClient.getState(create(ApplicationServiceGetStateRequestSchema, { applicationUid: app.uid }))
          .then(res => [app.uid, res.data] as [string, ApplicationState | undefined])
          .catch(() => [app.uid, undefined] as [string, ApplicationState | undefined])
      );
      const resourcePromises = applications.map(app =>
        applicationClient.getResource(create(ApplicationServiceGetResourceRequestSchema, { applicationUid: app.uid }))
          .then(res => [app.uid, res.data] as [string, ApplicationResource | undefined])
          .catch(() => [app.uid, undefined] as [string, ApplicationResource | undefined])
      );
      const stateResults = await Promise.all(statePromises);
      const resourceResults = await Promise.all(resourcePromises);

      const applicationStates = new Map<string, ApplicationState>();
      stateResults.forEach(([uid, state]) => {
        if (state) applicationStates.set(uid, state);
      });
      const applicationResources = new Map<string, ApplicationResource>();
      resourceResults.forEach(([uid, resource]) => {
        if (resource) applicationResources.set(uid, resource);
      });

      logger.info('Initial data loaded:', {
        applications: applications.length,
        labels: labelsRes.data?.length || 0,
        nodes: nodesRes.data?.length || 0,
        users: usersRes.data?.length || 0,
        roles: rolesRes.data?.length || 0,
        applicationStates: applicationStates.size,
        applicationResources: applicationResources.size,
      });

      const newData = {
        applications,
        applicationStates,
        applicationResources,
        applicationTasks: new Map(),
        labels: labelsRes.data || [],
        nodes: nodesRes.data || [],
        users: usersRes.data || [],
        roles: rolesRes.data || [],
      };

      currentDataRef.current = newData;
      setData(newData);
      notifySubscribers(newData);
      addNotification('info', 'Data refreshed successfully');
    } catch (err) {
      const errorMessage = `Failed to fetch data: ${err}`;
      setError(errorMessage);
      addNotification('error', 'Failed to refresh data', String(err));
      logger.error('Data refresh failed:', err);
    }
  }, [isAuthenticated, notifySubscribers, addNotification]);

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
          // Refresh data to ensure UI is updated
          setTimeout(() => refreshData(), 500);
          break;
        case 'ApplicationServiceDeallocateRequest':
          response = await applicationClient.deallocate(request);
          addNotification('info', 'Application deallocated successfully');
          break;
        case 'ApplicationServiceGetResourceRequest':
          response = await applicationClient.getResource(request);
          addNotification('info', 'Resource access information retrieved');
          break;
        case 'GateProxySSHServiceGetResourceAccessRequest':
          response = await gateProxySSHClient.getResourceAccess(request);
          addNotification('info', 'Resource access information retrieved');
          break;
        case 'LabelServiceCreateRequest':
          response = await labelClient.create(request);
          addNotification('info', 'Label created successfully');
          // Refresh data to ensure UI is updated
          setTimeout(() => refreshData(), 500);
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
  }, [addNotification, refreshData]);

  // Connect to streaming
  const connect = useCallback(async () => {
    if (isReconnectingRef.current || !isAuthenticated) return;

    try {
      logger.info('Connecting to streaming service...');
      setConnectionStatus('connecting');
      setError(null);

      // Create subscription request
      const subscribeRequest = create(StreamingServiceSubscribeRequestSchema, {
        subscriptionTypes: [
          SubscriptionType.APPLICATION,
          SubscriptionType.APPLICATION_STATE,
          SubscriptionType.APPLICATION_RESOURCE,
          SubscriptionType.APPLICATION_TASK,
          SubscriptionType.LABEL,
          SubscriptionType.NODE,
          SubscriptionType.USER,
          SubscriptionType.ROLE,
        ],
      });

      logger.debug('Subscribing with request:', subscribeRequest);

      // Create subscription stream
      subscribeStreamRef.current = streamingClient.subscribe(subscribeRequest);

      setConnectionStatus('connected');
      setIsConnected(true);
      logger.info('Successfully connected to streaming service');
      addNotification('info', 'Connected to real-time updates');

      // Process subscription updates
      for await (const response of subscribeStreamRef.current) {
        handleSubscriptionUpdate(response);
      }

    } catch (err) {
      logger.error('Streaming connection error:', err);
      const errorMsg = `Connection error: ${err}`;
      setError(errorMsg);
      addNotification('error', 'Connection failed', errorMsg);
      setConnectionStatus('error');
      setIsConnected(false);

      // Schedule reconnection
      if (!isReconnectingRef.current) {
        isReconnectingRef.current = true;
        logger.info('Scheduling reconnection in 5 seconds...');
        reconnectTimeoutRef.current = window.setTimeout(() => {
          isReconnectingRef.current = false;
          connect();
        }, 5000);
      }
    }
  }, [isAuthenticated, handleSubscriptionUpdate, addNotification]);

  // Disconnect from streaming
  const disconnect = useCallback(() => {
    if (!isConnected) {
      return
    }
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

    setIsConnected(false);
    setConnectionStatus('disconnected');
    addNotification('info', 'Disconnected from streaming service');
  }, [addNotification]);

  // Effect to manage connection
  useEffect(() => {
    if (isAuthenticated && user) {
      logger.info('User authenticated, initializing streaming...');
      refreshData();
      connect();
    } else {
      logger.info('User not authenticated, disconnecting...');
      disconnect();
    }

    return () => {
      disconnect();
    };
  }, [isAuthenticated, user, refreshData, connect, disconnect]);

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
    refreshData,
  };

  return (
    <StreamingContext.Provider value={contextValue}>
      {children}
    </StreamingContext.Provider>
  );
};
