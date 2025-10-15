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
import { tokenStorage } from '../auth';
import { ApplicationService } from '../../../gen/aquarium/v2/application_pb';
import { LabelService } from '../../../gen/aquarium/v2/label_pb';
import { NodeService } from '../../../gen/aquarium/v2/node_pb';
import { UserService } from '../../../gen/aquarium/v2/user_pb';
import { RoleService } from '../../../gen/aquarium/v2/role_pb';
import { GateProxySSHService } from '../../../gen/aquarium/v2/gate_proxyssh_access_pb';
import { StreamingService } from '../../../gen/aquarium/v2/streaming_pb';

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
            const { authService } = await import('../auth');
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
export const streamingClient = createClient(StreamingService, transport);
export const applicationClient = createClient(ApplicationService, transport);
export const labelClient = createClient(LabelService, transport);
export const nodeClient = createClient(NodeService, transport);
export const userClient = createClient(UserService, transport);
export const roleClient = createClient(RoleService, transport);
export const gateProxySSHClient = createClient(GateProxySSHService, transport);

