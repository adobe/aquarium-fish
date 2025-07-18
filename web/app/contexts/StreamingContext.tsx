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
import type { ReactNode } from 'react';
import { createClient } from '@connectrpc/connect';
import { createGrpcWebTransport } from '@connectrpc/connect-web';
import { create } from '@bufbuild/protobuf';
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
  type ApplicationServiceListResponse,
  type Application,
  type ApplicationState,
  type ApplicationResource,
  type ApplicationTask,
} from '../../gen/aquarium/v2/application_pb';
import {
  LabelService,
  LabelServiceListRequestSchema,
  type LabelServiceListResponse,
  type Label,
} from '../../gen/aquarium/v2/label_pb';
import {
  NodeService,
  NodeServiceListRequestSchema,
  type NodeServiceListResponse,
  type Node,
} from '../../gen/aquarium/v2/node_pb';
import {
  UserService,
  UserServiceListRequestSchema,
  type UserServiceListResponse,
  type User,
} from '../../gen/aquarium/v2/user_pb';
import {
  RoleService,
  RoleServiceListRequestSchema,
  type RoleServiceListResponse,
  type Role,
} from '../../gen/aquarium/v2/role_pb';

// Transport configuration
const transport = createGrpcWebTransport({
  baseUrl: typeof window !== 'undefined' ? `${window.location.origin}/grpc` : 'http://localhost:8001/grpc',
  interceptors: [
    (next) => async (req) => {
      // Add auth header if available
      const tokens = tokenStorage.getTokens();
      if (tokens && tokens.accessToken) {
        req.header.set('authorization', `Bearer ${tokens.accessToken}`);
      }
      return next(req);
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
  const [data, setData] = useState<StreamingData>({
    applications: [],
    applicationStates: new Map(),
    applicationResources: new Map(),
    applicationTasks: new Map(),
    labels: [],
    nodes: [],
    users: [],
    roles: [],
  });
  const [isConnected, setIsConnected] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('disconnected');
  const [error, setError] = useState<string | null>(null);

  // Refs for managing streams
  const connectStreamRef = useRef<AsyncIterable<StreamingServiceConnectResponse> | null>(null);
  const subscribeStreamRef = useRef<AsyncIterable<StreamingServiceSubscribeResponse> | null>(null);
  const callbacksRef = useRef<Set<DataUpdateCallback>>(new Set());
  const reconnectTimeoutRef = useRef<number | null>(null);
  const isReconnectingRef = useRef(false);
  const pendingRequestsRef = useRef<Map<string, { resolve: (value: any) => void; reject: (reason?: any) => void }>>(new Map());
  const requestQueueRef = useRef<StreamingServiceConnectRequest[]>([]);
  const connectWriterRef = useRef<WritableStream<StreamingServiceConnectRequest> | null>(null);

  // Subscribe to data updates
  const subscribe = useCallback((callback: DataUpdateCallback) => {
    callbacksRef.current.add(callback);
    // Immediately call with current data
    callback(data);

    return () => {
      callbacksRef.current.delete(callback);
    };
  }, [data]);

  // Notify all subscribers
  const notifySubscribers = useCallback((newData: StreamingData) => {
    callbacksRef.current.forEach(callback => callback(newData));
  }, []);

  // Initial data fetch
  const refreshData = useCallback(async () => {
    if (!isAuthenticated) return;

    try {
      const [appsRes, labelsRes, nodesRes, usersRes, rolesRes] = await Promise.all([
        applicationClient.list(create(ApplicationServiceListRequestSchema)),
        labelClient.list(create(LabelServiceListRequestSchema)),
        nodeClient.list(create(NodeServiceListRequestSchema)),
        userClient.list(create(UserServiceListRequestSchema)),
        roleClient.list(create(RoleServiceListRequestSchema)),
      ]);

      const newData = {
        applications: appsRes.data || [],
        applicationStates: new Map(),
        applicationResources: new Map(),
        applicationTasks: new Map(),
        labels: labelsRes.data || [],
        nodes: nodesRes.data || [],
        users: usersRes.data || [],
        roles: rolesRes.data || [],
      };

      setData(newData);
      notifySubscribers(newData);
    } catch (err) {
      console.error('Failed to fetch initial data:', err);
      setError(`Failed to fetch data: ${err}`);
    }
  }, [isAuthenticated, notifySubscribers]);

  // Handle subscription updates
  const handleSubscriptionUpdate = useCallback((response: StreamingServiceSubscribeResponse) => {
    setData(prevData => {
      const newData = { ...prevData };

      try {
        switch (response.objectType) {
          case SubscriptionType.APPLICATION: {
            const app = response.objectData?.value as unknown as Application;
            if (app) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.applications = newData.applications.filter(a => a.uid !== app.uid);
                  newData.applications.push(app);
                  break;
                case ChangeType.REMOVED:
                  newData.applications = newData.applications.filter(a => a.uid !== app.uid);
                  break;
              }
            }
            break;
          }

          case SubscriptionType.APPLICATION_STATE: {
            const state = response.objectData?.value as unknown as ApplicationState;
            if (state) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.applicationStates.set(state.applicationUid, state);
                  break;
                case ChangeType.REMOVED:
                  newData.applicationStates.delete(state.applicationUid);
                  break;
              }
            }
            break;
          }

          case SubscriptionType.APPLICATION_RESOURCE: {
            const resource = response.objectData?.value as unknown as ApplicationResource;
            if (resource) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.applicationResources.set(resource.applicationUid, resource);
                  break;
                case ChangeType.REMOVED:
                  newData.applicationResources.delete(resource.applicationUid);
                  break;
              }
            }
            break;
          }

          case SubscriptionType.LABEL: {
            const label = response.objectData?.value as unknown as Label;
            if (label) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.labels = newData.labels.filter(l => l.uid !== label.uid);
                  newData.labels.push(label);
                  break;
                case ChangeType.REMOVED:
                  newData.labels = newData.labels.filter(l => l.uid !== label.uid);
                  break;
              }
            }
            break;
          }

          case SubscriptionType.NODE: {
            const node = response.objectData?.value as unknown as Node;
            if (node) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.nodes = newData.nodes.filter(n => n.uid !== node.uid);
                  newData.nodes.push(node);
                  break;
                case ChangeType.REMOVED:
                  newData.nodes = newData.nodes.filter(n => n.uid !== node.uid);
                  break;
              }
            }
            break;
          }

          case SubscriptionType.USER: {
            const user = response.objectData?.value as unknown as User;
            if (user) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.users = newData.users.filter(u => u.name !== user.name);
                  newData.users.push(user);
                  break;
                case ChangeType.REMOVED:
                  newData.users = newData.users.filter(u => u.name !== user.name);
                  break;
              }
            }
            break;
          }

          case SubscriptionType.ROLE: {
            const role = response.objectData?.value as unknown as Role;
            if (role) {
              switch (response.changeType) {
                case ChangeType.CREATED:
                case ChangeType.UPDATED:
                  newData.roles = newData.roles.filter(r => r.name !== role.name);
                  newData.roles.push(role);
                  break;
                case ChangeType.REMOVED:
                  newData.roles = newData.roles.filter(r => r.name !== role.name);
                  break;
              }
            }
            break;
          }
        }
      } catch (err) {
        console.error('Error processing subscription update:', err);
      }

      notifySubscribers(newData);
      return newData;
    });
  }, [notifySubscribers]);

  // Handle Connect stream responses
  const handleConnectResponse = useCallback((response: StreamingServiceConnectResponse) => {
    const { requestId, error: streamError, responseData } = response;

    const pendingRequest = pendingRequestsRef.current.get(requestId);
    if (pendingRequest) {
      pendingRequestsRef.current.delete(requestId);

      if (streamError) {
        pendingRequest.reject(new Error(`Stream error: ${streamError.message}`));
      } else {
        pendingRequest.resolve(responseData);
      }
    }
  }, []);

  // Send request through Connect stream
  const sendRequest = useCallback(async (request: any, requestType: string): Promise<any> => {
    // For now, fall back to direct service calls until bidirectional streaming is fully implemented
    // This is a temporary solution to make the UI functional
    console.log('Sending request through direct service call:', requestType);

    try {
      switch (requestType) {
        case 'ApplicationServiceCreateRequest':
          return await applicationClient.create(request);
        case 'ApplicationServiceDeallocateRequest':
          return await applicationClient.deallocate(request);
        case 'LabelServiceCreateRequest':
          return await labelClient.create(request);
        case 'LabelServiceRemoveRequest':
          return await labelClient.remove(request);
        case 'UserServiceCreateRequest':
          return await userClient.create(request);
        case 'UserServiceUpdateRequest':
          return await userClient.update(request);
        default:
          throw new Error(`Unsupported request type: ${requestType}`);
      }
    } catch (err) {
      console.error('Request failed:', err);
      throw err;
    }
  }, []);

  // Connect to streaming
  const connect = useCallback(async () => {
    if (isReconnectingRef.current || !isAuthenticated) return;

    try {
      setConnectionStatus('connecting');
      setError(null);

      // Create subscription reques
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

      // Create subscription stream
      subscribeStreamRef.current = streamingClient.subscribe(subscribeRequest);

      setConnectionStatus('connected');
      setIsConnected(true);

      // Process subscription updates
      for await (const response of subscribeStreamRef.current) {
        handleSubscriptionUpdate(response);
      }

    } catch (err) {
      console.error('Streaming connection error:', err);
      setError(`Connection error: ${err}`);
      setConnectionStatus('error');
      setIsConnected(false);

      // Schedule reconnection
      if (!isReconnectingRef.current) {
        isReconnectingRef.current = true;
        reconnectTimeoutRef.current = window.setTimeout(() => {
          isReconnectingRef.current = false;
          connect();
        }, 5000);
      }
    }
  }, [isAuthenticated, handleSubscriptionUpdate]);

  // Disconnect from streaming
  const disconnect = useCallback(() => {
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
  }, []);

  // Effect to manage connection
  useEffect(() => {
    if (isAuthenticated && user) {
      refreshData();
      connect();
    } else {
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
