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
import { labelsService } from '../api/labels.service';
import type { Label } from '../../../../gen/aquarium/v2/label_pb';

export function useLabels() {
  const { subscribe } = useStreaming();
  const [labels, setLabels] = useState<Label[]>([]);

  useEffect(() => {
    return subscribe((streamData) => {
      setLabels(streamData.labels);
    });
  }, [subscribe]);

  return { labels };
}

export function useLabelCreate() {
  const { sendNotification } = useNotification();

  const create = useCallback(async (labelData: Label): Promise<void> => {
    try {
      const label: Label = {
        ...labelData,
        uid: crypto.randomUUID(),
      };

      await labelsService.create(label);
      sendNotification('info', 'Label created successfully');
    } catch (error) {
      sendNotification('error', 'Failed to create label', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { create };
}

export function useLabelRemove() {
  const { sendNotification } = useNotification();

  const remove = useCallback(async (labelUid: string): Promise<void> => {
    try {
      await labelsService.remove(labelUid);
      sendNotification('info', 'Label deleted successfully');
    } catch (error) {
      sendNotification('error', 'Failed to delete label', String(error));
      throw error;
    }
  }, [sendNotification]);

  return { remove };
}

