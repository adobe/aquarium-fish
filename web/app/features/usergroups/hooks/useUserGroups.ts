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
import { usergroupsService } from '../api/usergroups.service';
import type { UserGroup } from '../../../../gen/aquarium/v2/user_pb';

export function useUserGroups() {
  const { subscribe } = useStreaming();
  const [usergroups, setUserGroups] = useState<UserGroup[]>([]);

  useEffect(() => {
    return subscribe((streamData) => {
      setUserGroups(streamData.usergroups);
    });
  }, [subscribe]);

  return { usergroups };
}

export function useUserGroupCreate() {
  const { sendNotification } = useNotification();

  const create = useCallback(async (groupData: UserGroup): Promise<void> => {
    try {
      await usergroupsService.create(groupData);
      sendNotification('info', 'User group created successfully');
    } catch (error) {
      sendNotification('error', 'Failed to create user group', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { create };
}

export function useUserGroupUpdate() {
  const { sendNotification } = useNotification();

  const update = useCallback(async (groupData: UserGroup): Promise<void> => {
    try {
      await usergroupsService.update(groupData);
      sendNotification('info', 'User group updated successfully');
    } catch (error) {
      sendNotification('error', 'Failed to update user group', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { update };
}

export function useUserGroupRemove() {
  const { sendNotification } = useNotification();

  const remove = useCallback(async (groupName: string): Promise<void> => {
    try {
      await usergroupsService.remove(groupName);
      sendNotification('info', 'User group deleted successfully');
    } catch (error) {
      sendNotification('error', 'Failed to delete user group', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { remove };
}

