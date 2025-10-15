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

import { create } from '@bufbuild/protobuf';
import { roleClient } from '../../../lib/api/client';
import {
  RoleServiceListRequestSchema,
  RoleServiceCreateRequestSchema,
  RoleServiceUpdateRequestSchema,
  RoleServiceRemoveRequestSchema,
  type Role,
} from '../../../../gen/aquarium/v2/role_pb';

export class RolesService {
  async list(): Promise<Role[]> {
    const response = await roleClient.list(create(RoleServiceListRequestSchema));
    return response.data || [];
  }

  async create(role: Role): Promise<void> {
    const request = create(RoleServiceCreateRequestSchema, {
      role,
    });
    await roleClient.create(request);
  }

  async update(role: Role): Promise<void> {
    const request = create(RoleServiceUpdateRequestSchema, {
      role,
    });
    await roleClient.update(request);
  }

  async remove(roleName: string): Promise<void> {
    const request = create(RoleServiceRemoveRequestSchema, {
      roleName,
    });
    await roleClient.remove(request);
  }
}

export const rolesService = new RolesService();

