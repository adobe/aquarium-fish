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

import { applicationsService } from '../../../features/applications/api/applications.service';
import { PermService, PermApplication } from '../../../../gen/permissions/permissions_grpc';
import { logger } from '../connection';
import type { StreamingData } from '../types';

export class ApplicationsProvider {
  async fetchApplications(
    fetchedDataTypes: Set<string>,
    setData: (updater: (prevData: StreamingData) => StreamingData) => void,
    notifySubscribers: (data: StreamingData) => void,
    currentDataRef: React.MutableRefObject<StreamingData>,
    hasPermission: (service: number, action: number) => boolean,
    addNotification: (type: 'error' | 'warning' | 'info', message: string, details?: string) => void
  ): Promise<void> {
    // Check if applications have already been fetched in this session
    if (fetchedDataTypes.has('applications')) {
      logger.debug('Applications already fetched in this session, skipping');
      return;
    }

    const canListApplications = hasPermission(PermService.Application, PermApplication.List);
    if (!canListApplications) {
      logger.info('User does not have permission to list applications');
      return;
    }

    try {
      logger.info('Fetching applications...');
      const applications = await applicationsService.list();

      setData(prevData => {
        const newData = { ...prevData, applications };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      logger.info('Applications fetched:', applications.length);
      fetchedDataTypes.add('applications');
    } catch (err) {
      logger.error('Failed to fetch applications:', err);
      addNotification('error', 'Failed to fetch applications', String(err));
    }
  }

  async fetchApplicationStates(
    fetchedDataTypes: Set<string>,
    setData: (updater: (prevData: StreamingData) => StreamingData) => void,
    notifySubscribers: (data: StreamingData) => void,
    currentDataRef: React.MutableRefObject<StreamingData>,
    hasPermission: (service: number, action: number) => boolean,
    addNotification: (type: 'error' | 'warning' | 'info', message: string, details?: string) => void
  ): Promise<void> {
    // Check if application states have already been fetched in this session
    if (fetchedDataTypes.has('applicationStates')) {
      logger.debug('ApplicationStates already fetched in this session, skipping');
      return;
    }

    const canListApplicationState = hasPermission(PermService.Application, PermApplication.ListState);
    if (!canListApplicationState) {
      logger.info('User does not have permission to list application states');
      return;
    }

    try {
      logger.info('Fetching all application states...');
      const states = await applicationsService.listStates();
      const newStates = new Map<string, any>();

      states.forEach(state => {
        if (state.applicationUid) {
          newStates.set(state.applicationUid, state);
        }
      });

      setData(prevData => {
        const newData = {
          ...prevData,
          applicationStates: new Map([...prevData.applicationStates, ...newStates])
        };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      logger.info('Application states fetched:', newStates.size);
      fetchedDataTypes.add('applicationStates');
    } catch (err) {
      logger.error('Failed to fetch application states:', err);
      addNotification('error', 'Failed to fetch application states', String(err));
    }
  }

  async fetchApplicationResources(
    fetchedDataTypes: Set<string>,
    setData: (updater: (prevData: StreamingData) => StreamingData) => void,
    notifySubscribers: (data: StreamingData) => void,
    currentDataRef: React.MutableRefObject<StreamingData>,
    hasPermission: (service: number, action: number) => boolean,
    addNotification: (type: 'error' | 'warning' | 'info', message: string, details?: string) => void
  ): Promise<void> {
    // Check if application resources have already been fetched in this session
    if (fetchedDataTypes.has('applicationResources')) {
      logger.debug('ApplicationResources already fetched in this session, skipping');
      return;
    }

    const canListApplicationResource = hasPermission(PermService.Application, PermApplication.ListResource);
    if (!canListApplicationResource) {
      logger.info('User does not have permission to list application resources');
      return;
    }

    try {
      logger.info('Fetching all application resources...');
      const resources = await applicationsService.listResources();
      const newResources = new Map<string, any>();

      resources.forEach(resource => {
        if (resource.applicationUid) {
          newResources.set(resource.applicationUid, resource);
        }
      });

      setData(prevData => {
        const newData = {
          ...prevData,
          applicationResources: new Map([...prevData.applicationResources, ...newResources])
        };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      logger.info('Application resources fetched:', newResources.size);
      fetchedDataTypes.add('applicationResources');
    } catch (err) {
      logger.error('Failed to fetch application resources:', err);
      addNotification('error', 'Failed to fetch application resources', String(err));
    }
  }
}

