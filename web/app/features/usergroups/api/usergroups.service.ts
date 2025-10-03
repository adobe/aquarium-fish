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
import { userClient } from '../../../lib/api/client';
import {
  UserServiceListGroupRequestSchema,
  UserServiceCreateGroupRequestSchema,
  UserServiceUpdateGroupRequestSchema,
  UserServiceRemoveGroupRequestSchema,
  type UserGroup,
} from '../../../../gen/aquarium/v2/user_pb';

export class UserGroupsService {
  async list(): Promise<UserGroup[]> {
    const response = await userClient.listGroup(create(UserServiceListGroupRequestSchema));
    return response.data || [];
  }

  async create(usergroup: UserGroup): Promise<void> {
    const request = create(UserServiceCreateGroupRequestSchema, {
      usergroup,
    });
    await userClient.createGroup(request);
  }

  async update(usergroup: UserGroup): Promise<void> {
    const request = create(UserServiceUpdateGroupRequestSchema, {
      usergroup,
    });
    await userClient.updateGroup(request);
  }

  async remove(groupName: string): Promise<void> {
    const request = create(UserServiceRemoveGroupRequestSchema, {
      groupName,
    });
    await userClient.removeGroup(request);
  }
}

export const usergroupsService = new UserGroupsService();

