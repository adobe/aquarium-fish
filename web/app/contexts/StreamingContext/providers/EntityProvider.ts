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

import { logger } from '../connection';
import type { StreamingData } from '../types';

/**
 * Generic entity provider for simple list-based entities
 */
export class EntityProvider<T> {
  constructor(
    private entityName: string,
    private dataKey: keyof StreamingData,
    private fetchFn: () => Promise<T[]>,
    private permissionCheck: { service: number; action: number }
  ) {}

  async fetch(
    fetchedDataTypes: Set<string>,
    setData: (updater: (prevData: StreamingData) => StreamingData) => void,
    notifySubscribers: (data: StreamingData) => void,
    currentDataRef: React.MutableRefObject<StreamingData>,
    hasPermission: (service: number, action: number) => boolean,
    addNotification: (type: 'error' | 'warning' | 'info', message: string, details?: string) => void
  ): Promise<void> {
    // Check if already fetched in this session
    if (fetchedDataTypes.has(this.entityName)) {
      logger.debug(`${this.entityName} already fetched in this session, skipping`);
      return;
    }

    const canList = hasPermission(this.permissionCheck.service, this.permissionCheck.action);
    if (!canList) {
      logger.info(`User does not have permission to list ${this.entityName}`);
      return;
    }

    try {
      logger.info(`Fetching ${this.entityName}...`);
      const data = await this.fetchFn();

      setData(prevData => {
        const newData = { ...prevData, [this.dataKey]: data };
        currentDataRef.current = newData;
        notifySubscribers(newData);
        return newData;
      });

      logger.info(`${this.entityName} fetched:`, data.length);
      fetchedDataTypes.add(this.entityName);
    } catch (err) {
      logger.error(`Failed to fetch ${this.entityName}:`, err);
      addNotification('error', `Failed to fetch ${this.entityName}`, String(err));
    }
  }
}

