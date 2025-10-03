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
import { Code, ConnectError } from '@connectrpc/connect';
import { streamingClient } from '../../lib/api/client';
import {
  StreamingServiceSubscribeRequestSchema,
  type StreamingServiceSubscribeResponse,
  SubscriptionType,
} from '../../../gen/aquarium/v2/streaming_pb';

// Enhanced logging utility
export const logger = {
  debug: (message: string, ...args: any[]) => {
    console.debug(`[StreamingContext] ${message}`, ...args);
  },
  info: (message: string, ...args: any[]) => {
    console.info(`[StreamingContext] ${message}`, ...args);
  },
  warn: (message: string, ...args: any[]) => {
    console.warn(`[StreamingContext] ${message}`, ...args);
  },
  error: (message: string, ...args: any[]) => {
    console.error(`[StreamingContext] ${message}`, ...args);
  },
  subscription: (update: StreamingServiceSubscribeResponse) => {
    console.debug(`[StreamingContext] SUBSCRIPTION UPDATE`, update);
  }
};

export class StreamingConnection {
  private controller = new AbortController();
  private subscribeStream: AsyncIterable<StreamingServiceSubscribeResponse> | null = null;
  private reconnectTimeout: number | null = null;
  private isReconnecting = false;

  async connect(
    subscriptionTypes: SubscriptionType[],
    onUpdate: (response: StreamingServiceSubscribeResponse) => void,
    onError: (error: string) => void,
    onConnect: () => void,
    onDisconnect: () => void
  ): Promise<void> {
    if (this.isReconnecting) return;

    try {
      logger.info('Connecting to streaming service...');

      const subscribeRequest = create(StreamingServiceSubscribeRequestSchema, {
        subscriptionTypes,
      });

      logger.debug('Subscribing with request:', subscribeRequest);

      // Create subscription stream with abort signal
      this.subscribeStream = streamingClient.subscribe(subscribeRequest, { signal: this.controller.signal });

      onConnect();
      logger.info('Successfully connected to streaming service');

      // Process subscription updates
      for await (const response of this.subscribeStream) {
        onUpdate(response);
      }
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Canceled) {
        // This is a valid abort due to logout - no need to panic
        return;
      }
      logger.error('Streaming connection error:', err);
      const errorMsg = `Connection error: ${err}`;
      onError(errorMsg);
      onDisconnect();

      // Schedule reconnection
      if (!this.isReconnecting) {
        this.isReconnecting = true;
        logger.info('Scheduling reconnection in 10 seconds...');
        this.reconnectTimeout = window.setTimeout(() => {
          this.isReconnecting = false;
          this.connect(subscriptionTypes, onUpdate, onError, onConnect, onDisconnect);
        }, 10000);
      }
    }
  }

  disconnect(): void {
    logger.info('Disconnecting from streaming service...');

    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }

    this.subscribeStream = null;
    this.isReconnecting = false;

    if (this.controller) {
      this.controller.abort("Disconnected by client");
      // Create new controller for next connection
      this.controller = new AbortController();
    }
  }

  isConnectedState(): boolean {
    return this.subscribeStream !== null;
  }
}

