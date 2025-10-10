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
import { labelClient } from '../../../lib/api/client';
import {
  LabelServiceListRequestSchema,
  LabelServiceCreateRequestSchema,
  LabelServiceUpdateRequestSchema,
  LabelServiceRemoveRequestSchema,
  type Label,
} from '../../../../gen/aquarium/v2/label_pb';

export class LabelsService {
  async list(): Promise<Label[]> {
    const response = await labelClient.list(create(LabelServiceListRequestSchema));
    return response.data || [];
  }

  async create(label: Label): Promise<void> {
    const request = create(LabelServiceCreateRequestSchema, {
      label,
    });
    await labelClient.create(request);
  }

  async update(label: Label): Promise<void> {
    const request = create(LabelServiceUpdateRequestSchema, {
      label,
    });
    await labelClient.update(request);
  }

  async remove(labelUid: string): Promise<void> {
    const request = create(LabelServiceRemoveRequestSchema, {
      labelUid,
    });
    await labelClient.remove(request);
  }
}

export const labelsService = new LabelsService();

