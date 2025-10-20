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
import { nodesService } from '../api/nodes.service';
import type { Node } from '../../../../gen/aquarium/v2/node_pb';

export function useNodes() {
  const { subscribe } = useStreaming();
  const [nodes, setNodes] = useState<Node[]>([]);

  useEffect(() => {
    return subscribe((streamData) => {
      setNodes(streamData.nodes);
    });
  }, [subscribe]);

  return { nodes };
}

export function useThisNode() {
  const { sendNotification } = useNotification();
  const [thisNode, setThisNode] = useState<Node | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const fetch = useCallback(async () => {
    try {
      setIsLoading(true);
      const node = await nodesService.getThis();
      setThisNode(node);
    } catch (error) {
      sendNotification('error', 'Failed to fetch current node', String(error));
    } finally {
      setIsLoading(false);
    }
  }, [sendNotification]);

  return { thisNode, fetch, isLoading };
}

export function useNodeMaintenance() {
  const { sendNotification } = useNotification();
  const [isLoading, setIsLoading] = useState(false);

  const setMaintenance = useCallback(async (maintenance?: boolean, shutdown?: boolean, shutdownDelay?: string): Promise<boolean> => {
    try {
      setIsLoading(true);
      const success = await nodesService.setMaintenance(maintenance, shutdown, shutdownDelay);

      if (success) {
        const action = shutdown ? 'shutdown' : (maintenance ? 'maintenance mode enabled' : 'maintenance mode disabled');
        sendNotification('info', `Node ${action} successfully`);
      } else {
        sendNotification('error', 'Failed to set node maintenance', 'The operation was not successful');
      }

      return success;
    } catch (error) {
      sendNotification('error', 'Failed to set node maintenance', String(error));
      return false;
    } finally {
      setIsLoading(false);
    }
  }, [sendNotification]);

  return { setMaintenance, isLoading };
}

