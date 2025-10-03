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
import { applicationClient } from '../../../lib/api/client';
import {
  ApplicationServiceListRequestSchema,
  ApplicationServiceListStateRequestSchema,
  ApplicationServiceListResourceRequestSchema,
  ApplicationServiceCreateRequestSchema,
  ApplicationServiceDeallocateRequestSchema,
  type Application,
  type ApplicationState,
  type ApplicationResource,
} from '../../../../gen/aquarium/v2/application_pb';

export class ApplicationsService {
  async list(): Promise<Application[]> {
    const response = await applicationClient.list(create(ApplicationServiceListRequestSchema));
    return response.data || [];
  }

  async listStates(): Promise<ApplicationState[]> {
    const response = await applicationClient.listState(create(ApplicationServiceListStateRequestSchema));
    return response.data || [];
  }

  async listResources(): Promise<ApplicationResource[]> {
    const response = await applicationClient.listResource(create(ApplicationServiceListResourceRequestSchema));
    return response.data || [];
  }

  async create(application: Application): Promise<void> {
    const request = create(ApplicationServiceCreateRequestSchema, {
      application,
    });
    await applicationClient.create(request);
  }

  async deallocate(applicationUid: string): Promise<void> {
    const request = create(ApplicationServiceDeallocateRequestSchema, {
      applicationUid,
    });
    await applicationClient.deallocate(request);
  }
}

export const applicationsService = new ApplicationsService();

