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

import { createClient } from '@connectrpc/connect';
import { createGrpcWebTransport } from '@connectrpc/connect-web';
import { create } from '@bufbuild/protobuf';
import { tokenStorage } from './auth';

import {
  ApplicationService,
  ApplicationServiceCreateRequestSchema,
  ApplicationServiceDeallocateRequestSchema,
  ApplicationServiceGetRequestSchema,
  ApplicationServiceGetStateRequestSchema,
  ApplicationServiceGetResourceRequestSchema,
  ApplicationServiceListRequestSchema,
  ApplicationServiceCreateTaskRequestSchema,
  ApplicationServiceListTaskRequestSchema,
  type ApplicationServiceCreateRequest,
  type ApplicationServiceDeallocateRequest,
  type ApplicationServiceGetRequest,
  type ApplicationServiceGetStateRequest,
  type ApplicationServiceGetResourceRequest,
  type ApplicationServiceListRequest,
  type ApplicationServiceCreateTaskRequest,
  type ApplicationServiceListTaskRequest,
  type Application,
  type ApplicationState,
  type ApplicationResource,
  type ApplicationTask,
} from '../../gen/aquarium/v2/application_pb';

import {
  LabelService,
  LabelServiceCreateRequestSchema,
  LabelServiceRemoveRequestSchema,
  LabelServiceGetRequestSchema,
  LabelServiceListRequestSchema,
  type LabelServiceCreateRequest,
  type LabelServiceRemoveRequest,
  type LabelServiceGetRequest,
  type LabelServiceListRequest,
  type Label,
} from '../../gen/aquarium/v2/label_pb';

import {
  NodeService,
  NodeServiceListRequestSchema,
  NodeServiceGetThisRequestSchema,
  NodeServiceSetMaintenanceRequestSchema,
  type NodeServiceListRequest,
  type NodeServiceGetThisRequest,
  type NodeServiceSetMaintenanceRequest,
  type Node,
} from '../../gen/aquarium/v2/node_pb';

import {
  UserService,
  UserServiceCreateRequestSchema,
  UserServiceRemoveRequestSchema,
  UserServiceGetRequestSchema,
  UserServiceListRequestSchema,
  UserServiceUpdateRequestSchema,
  type UserServiceCreateRequest,
  type UserServiceRemoveRequest,
  type UserServiceGetRequest,
  type UserServiceListRequest,
  type UserServiceUpdateRequest,
  type User,
} from '../../gen/aquarium/v2/user_pb';

import {
  RoleService,
  RoleServiceCreateRequestSchema,
  RoleServiceRemoveRequestSchema,
  RoleServiceGetRequestSchema,
  RoleServiceListRequestSchema,
  RoleServiceUpdateRequestSchema,
  type RoleServiceCreateRequest,
  type RoleServiceRemoveRequest,
  type RoleServiceGetRequest,
  type RoleServiceListRequest,
  type RoleServiceUpdateRequest,
  type Role,
} from '../../gen/aquarium/v2/role_pb';

import {
  GateProxySSHService,
  GateProxySSHServiceGetResourceAccessRequestSchema,
  type GateProxySSHServiceGetResourceAccessRequest,
  type GateProxySSHServiceGetResourceAccessResponse,
} from '../../gen/aquarium/v2/gate_proxyssh_access_pb';

// Create transport with auth header and automatic token refresh
const transport = createGrpcWebTransport({
  baseUrl: typeof window !== 'undefined' ? `${window.location.origin}/grpc` : 'http://localhost:8001/grpc',
  interceptors: [
    (next) => async (req) => {
      let tokens = tokenStorage.getTokens();
      if (tokens?.accessToken) {
        req.header.set('authorization', `Bearer ${tokens.accessToken}`);
      }

      try {
        return await next(req);
      } catch (error: any) {
        // If we get an authentication error, try to refresh the token and retry once
        if (error?.code === 'unauthenticated' && tokens && !tokenStorage.isRefreshTokenExpired(tokens)) {
          try {
            const { authService } = await import('./auth');
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

// Create service clients
export const applicationService = createClient(ApplicationService, transport);
export const labelService = createClient(LabelService, transport);
export const nodeService = createClient(NodeService, transport);
export const userService = createClient(UserService, transport);
export const roleService = createClient(RoleService, transport);
export const gateProxySSHService = createClient(GateProxySSHService, transport);

// Application service functions
export const applicationServiceHelpers = {
  async list(): Promise<Application[]> {
    const response = await applicationService.list(create(ApplicationServiceListRequestSchema));
    return response.data || [];
  },

  async get(uid: string): Promise<Application | null> {
    const response = await applicationService.get(create(ApplicationServiceGetRequestSchema, { applicationUid: uid }));
    return response.data || null;
  },

  async create(application: Application): Promise<Application | null> {
    const response = await applicationService.create(create(ApplicationServiceCreateRequestSchema, { application }));
    return response.data || null;
  },

  async deallocate(uid: string): Promise<boolean> {
    const response = await applicationService.deallocate(create(ApplicationServiceDeallocateRequestSchema, { applicationUid: uid }));
    return response.status;
  },

  async getState(uid: string): Promise<ApplicationState | null> {
    const response = await applicationService.getState(create(ApplicationServiceGetStateRequestSchema, { applicationUid: uid }));
    return response.data || null;
  },

  async getResource(uid: string): Promise<ApplicationResource | null> {
    const response = await applicationService.getResource(create(ApplicationServiceGetResourceRequestSchema, { applicationUid: uid }));
    return response.data || null;
  },

  async listTasks(uid: string): Promise<ApplicationTask[]> {
    const response = await applicationService.listTask(create(ApplicationServiceListTaskRequestSchema, { applicationUid: uid }));
    return response.data || [];
  },

  async createTask(uid: string, task: ApplicationTask): Promise<ApplicationTask | null> {
    const response = await applicationService.createTask(create(ApplicationServiceCreateTaskRequestSchema, { applicationUid: uid, task }));
    return response.data || null;
  },

  async getResourceAccess(uid: string): Promise<GateProxySSHServiceGetResourceAccessResponse | null> {
    const response = await gateProxySSHService.getResourceAccess(create(GateProxySSHServiceGetResourceAccessRequestSchema, { applicationUid: uid }));
    return response || null;
  },
};

// Label service functions
export const labelServiceHelpers = {
  async list(name?: string, version?: string): Promise<Label[]> {
    const response = await labelService.list(create(LabelServiceListRequestSchema, { name, version }));
    return response.data || [];
  },

  async get(uid: string): Promise<Label | null> {
    const response = await labelService.get(create(LabelServiceGetRequestSchema, { labelUid: uid }));
    return response.data || null;
  },

  async create(label: Label): Promise<Label | null> {
    const response = await labelService.create(create(LabelServiceCreateRequestSchema, { label }));
    return response.data || null;
  },

  async delete(uid: string): Promise<boolean> {
    const response = await labelService.delete(create(LabelServiceRemoveRequestSchema, { labelUid: uid }));
    return response.status;
  },
};

// Node service functions
export const nodeServiceHelpers = {
  async list(): Promise<Node[]> {
    const response = await nodeService.list(create(NodeServiceListRequestSchema));
    return response.data || [];
  },

  async getThis(): Promise<Node | null> {
    const response = await nodeService.getThis(create(NodeServiceGetThisRequestSchema));
    return response.data || null;
  },

  async setMaintenance(maintenance?: boolean, shutdown?: boolean, shutdownDelay?: string): Promise<boolean> {
    const response = await nodeService.setMaintenance(create(NodeServiceSetMaintenanceRequestSchema, {
      maintenance,
      shutdown,
      shutdownDelay,
    }));
    return response.status;
  },
};

// User service functions
export const userServiceHelpers = {
  async list(): Promise<User[]> {
    const response = await userService.list(create(UserServiceListRequestSchema));
    return response.data || [];
  },

  async get(userName: string): Promise<User | null> {
    const response = await userService.get(create(UserServiceGetRequestSchema, { userName }));
    return response.data || null;
  },

  async create(user: User): Promise<User | null> {
    const response = await userService.create(create(UserServiceCreateRequestSchema, { user }));
    return response.data || null;
  },

  async update(user: User): Promise<User | null> {
    const response = await userService.update(create(UserServiceUpdateRequestSchema, { user }));
    return response.data || null;
  },

  async delete(userName: string): Promise<boolean> {
    const response = await userService.delete(create(UserServiceRemoveRequestSchema, { userName }));
    return response.status;
  },
};

// Role service functions
export const roleServiceHelpers = {
  async list(): Promise<Role[]> {
    const response = await roleService.list(create(RoleServiceListRequestSchema));
    return response.data || [];
  },

  async get(roleName: string): Promise<Role | null> {
    const response = await roleService.get(create(RoleServiceGetRequestSchema, { roleName }));
    return response.data || null;
  },

  async create(role: Role): Promise<Role | null> {
    const response = await roleService.create(create(RoleServiceCreateRequestSchema, { role }));
    return response.data || null;
  },

  async update(role: Role): Promise<Role | null> {
    const response = await roleService.update(create(RoleServiceUpdateRequestSchema, { role }));
    return response.data || null;
  },

  async delete(roleName: string): Promise<boolean> {
    const response = await roleService.delete(create(RoleServiceRemoveRequestSchema, { roleName }));
    return response.status;
  },
};

// Utility functions
export const utils = {
  // Format timestamp to human readable
  formatTimestamp(timestamp: { seconds: string } | undefined): string {
    if (!timestamp) return 'Unknown';
    const date = new Date(Number(timestamp.seconds) * 1000);
    return date.toLocaleString();
  },

  // Parse YAML safely
  parseYAML(yamlString: string): any {
    try {
      // Simple YAML parsing - in production, use a proper YAML parser
      return JSON.parse(yamlString);
    } catch (error) {
      throw new Error(`Invalid YAML: ${error}`);
    }
  },

  // Convert object to YAML string
  toYAML(obj: any): string {
    // Simple conversion - in production, use a proper YAML stringifier
    return JSON.stringify(obj, null, 2);
  },

  // Generate unique ID
  generateId(): string {
    return crypto.randomUUID();
  },

  // Debounce function
  debounce<T extends (...args: any[]) => any>(func: T, delay: number): T {
    let timeoutId: number;
    return ((...args: any[]) => {
      clearTimeout(timeoutId);
      timeoutId = setTimeout(() => func.apply(null, args), delay);
    }) as T;
  },

  // Format bytes to human readable
  formatBytes(bytes: number, decimals = 2): string {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
  },

  // Format duration
  formatDuration(duration: string): string {
    if (!duration) return 'Unknown';
    // Simple duration formatting - parse strings like "1h30m30s"
    const match = duration.match(/(\d+h)?(\d+m)?(\d+s)?/);
    if (!match) return duration;

    const hours = match[1] ? parseInt(match[1]) : 0;
    const minutes = match[2] ? parseInt(match[2]) : 0;
    const seconds = match[3] ? parseInt(match[3]) : 0;

    const parts = [];
    if (hours > 0) parts.push(`${hours}h`);
    if (minutes > 0) parts.push(`${minutes}m`);
    if (seconds > 0) parts.push(`${seconds}s`);

    return parts.join(' ') || '0s';
  },
};
