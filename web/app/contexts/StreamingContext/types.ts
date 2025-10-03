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

import type { Application, ApplicationState, ApplicationResource, ApplicationTask } from '../../../gen/aquarium/v2/application_pb';
import type { Label } from '../../../gen/aquarium/v2/label_pb';
import type { Node } from '../../../gen/aquarium/v2/node_pb';
import type { User, UserGroup } from '../../../gen/aquarium/v2/user_pb';
import type { Role } from '../../../gen/aquarium/v2/role_pb';

export interface StreamingData {
  applications: Application[];
  applicationStates: Map<string, ApplicationState>;
  applicationResources: Map<string, ApplicationResource>;
  applicationTasks: Map<string, ApplicationTask[]>;
  labels: Label[];
  nodes: Node[];
  users: User[];
  usergroups: UserGroup[];
  roles: Role[];
}

export interface DataUpdateCallback {
  (data: StreamingData): void;
}

export interface StreamingContextType {
  data: StreamingData;
  isConnected: boolean;
  connectionStatus: 'connecting' | 'connected' | 'disconnected' | 'error';
  error: string | null;
  subscribe: (callback: DataUpdateCallback) => () => void;
  // Individual data fetching functions
  fetchApplications: () => Promise<void>;
  fetchLabels: () => Promise<void>;
  fetchNodes: () => Promise<void>;
  fetchUsers: () => Promise<void>;
  fetchUserGroups: () => Promise<void>;
  fetchRoles: () => Promise<void>;
  fetchApplicationStates: () => Promise<void>;
  fetchApplicationResources: () => Promise<void>;
  // Utility functions
  resetFetchedDataTypes: () => void;
}

