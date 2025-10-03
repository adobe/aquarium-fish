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
import { usersService } from '../api/users.service';
import type { User } from '../../../../gen/aquarium/v2/user_pb';

export function useUsers() {
  const { subscribe } = useStreaming();
  const [users, setUsers] = useState<User[]>([]);

  useEffect(() => {
    return subscribe((streamData) => {
      setUsers(streamData.users);
    });
  }, [subscribe]);

  return { users };
}

export function useUserCreate() {
  const { sendNotification } = useNotification();

  const create = useCallback(async (userData: User): Promise<void> => {
    try {
      await usersService.create(userData);
      sendNotification('info', 'User created successfully');
    } catch (error) {
      sendNotification('error', 'Failed to create user', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { create };
}

export function useUserUpdate() {
  const { sendNotification } = useNotification();

  const update = useCallback(async (userData: User): Promise<void> => {
    try {
      await usersService.update(userData);
      sendNotification('info', 'User updated successfully');
    } catch (error) {
      sendNotification('error', 'Failed to update user', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { update };
}

export function useUserRemove() {
  const { sendNotification } = useNotification();

  const remove = useCallback(async (userName: string): Promise<void> => {
    try {
      await usersService.remove(userName);
      sendNotification('info', 'User deleted successfully');
    } catch (error) {
      sendNotification('error', 'Failed to delete user', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { remove };
}

