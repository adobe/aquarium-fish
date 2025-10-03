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

import { useState, useEffect, useCallback } from 'react';
import { useStreaming } from '../../../contexts/StreamingContext/index';
import { useNotification } from '../../../components/Notifications';
import { rolesService } from '../api/roles.service';
import type { Role } from '../../../../gen/aquarium/v2/role_pb';

export function useRoles() {
  const { subscribe } = useStreaming();
  const [roles, setRoles] = useState<Role[]>([]);

  useEffect(() => {
    return subscribe((streamData) => {
      setRoles(streamData.roles);
    });
  }, [subscribe]);

  return { roles };
}

export function useRoleCreate() {
  const { sendNotification } = useNotification();

  const create = useCallback(async (roleData: Role): Promise<void> => {
    try {
      await rolesService.create(roleData);
      sendNotification('info', 'Role created successfully');
    } catch (error) {
      sendNotification('error', 'Failed to create role', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { create };
}

export function useRoleUpdate() {
  const { sendNotification } = useNotification();

  const update = useCallback(async (roleData: Role): Promise<void> => {
    try {
      await rolesService.update(roleData);
      sendNotification('info', 'Role updated successfully');
    } catch (error) {
      sendNotification('error', 'Failed to update role', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { update };
}

export function useRoleRemove() {
  const { sendNotification } = useNotification();

  const remove = useCallback(async (roleName: string): Promise<void> => {
    try {
      await rolesService.remove(roleName);
      sendNotification('info', 'Role deleted successfully');
    } catch (error) {
      sendNotification('error', 'Failed to delete role', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { remove };
}

