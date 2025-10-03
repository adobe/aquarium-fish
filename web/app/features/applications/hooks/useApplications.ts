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
import { useAuth } from '../../../contexts/AuthContext';
import { useNotification } from '../../../components/Notifications';
import { applicationsService } from '../api/applications.service';
import { sshService } from '../api/ssh.service';
import type { ApplicationWithDetails } from '../types';
import type { Application } from '../../../../gen/aquarium/v2/application_pb';

export function useApplications() {
  const { data, subscribe } = useStreaming();
  const { user } = useAuth();
  const [applications, setApplications] = useState<ApplicationWithDetails[]>([]);

  useEffect(() => {
    return subscribe((streamData) => {
      const processed: ApplicationWithDetails[] = streamData.applications.map(app => {
        const state = streamData.applicationStates.get(app.uid);
        const resource = streamData.applicationResources.get(app.uid);
        const isUserOwned = user?.userName === app.ownerName;

        return {
          ...app,
          state,
          resource,
          isUserOwned,
        };
      });

      setApplications(processed);
    });
  }, [subscribe, user?.userName]);

  return { applications, labels: data.labels };
}

export function useApplicationCreate() {
  const { user } = useAuth();
  const { sendNotification } = useNotification();

  const create = useCallback(async (applicationData: Application): Promise<void> => {
    try {
      const application: Application = {
        ...applicationData,
        uid: crypto.randomUUID(),
        ownerName: user?.userName || '',
      };

      await applicationsService.create(application);
      sendNotification('info', 'Application created successfully');
    } catch (error) {
      sendNotification('error', 'Failed to create application', String(error));
      throw error;
    }
  }, [user?.userName, sendNotification]);

  return { create };
}

export function useApplicationDeallocate() {
  const { sendNotification } = useNotification();

  const deallocate = useCallback(async (applicationUid: string): Promise<void> => {
    try {
      await applicationsService.deallocate(applicationUid);
      sendNotification('info', 'Application deallocated successfully');
    } catch (error) {
      sendNotification('error', 'Failed to deallocate application', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { deallocate };
}

export function useApplicationSSH() {
  const { sendNotification } = useNotification();
  const [isLoading, setIsLoading] = useState(false);

  const getResourceAccess = useCallback(async (applicationResourceUid: string) => {
    try {
      setIsLoading(true);
      const response = await sshService.getResourceAccess(applicationResourceUid);
      sendNotification('info', 'Resource access information retrieved');
      return response.data;
    } catch (error) {
      sendNotification('error', 'Failed to get resource access', String(error));
      throw error;
    } finally {
      setIsLoading(false);
    }
  }, [sendNotification]);

  return { getResourceAccess, isLoading };
}

