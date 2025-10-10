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

import { fromBinary } from '@bufbuild/protobuf';
import {
  type StreamingServiceSubscribeResponse,
  SubscriptionType,
  ChangeType,
} from '../../../gen/aquarium/v2/streaming_pb';
import {
  ApplicationSchema,
  ApplicationStateSchema,
  ApplicationResourceSchema,
} from '../../../gen/aquarium/v2/application_pb';
import { LabelSchema } from '../../../gen/aquarium/v2/label_pb';
import { NodeSchema } from '../../../gen/aquarium/v2/node_pb';
import { UserSchema, UserGroupSchema } from '../../../gen/aquarium/v2/user_pb';
import { RoleSchema } from '../../../gen/aquarium/v2/role_pb';
import type { StreamingData } from './types';
import { logger } from './connection';

export function handleSubscriptionUpdate(
  response: StreamingServiceSubscribeResponse,
  prevData: StreamingData
): StreamingData {
  logger.subscription(response);

  const newData = { ...prevData };

  try {
    const binaryData = response.objectData?.value;
    if (!binaryData || !(binaryData instanceof Uint8Array)) {
      logger.warn('No binary data or invalid data type in subscription update');
      return prevData;
    }

    switch (response.objectType) {
      case SubscriptionType.APPLICATION: {
        const app = fromBinary(ApplicationSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.applications = newData.applications.filter(a => a.uid !== app.uid);
            newData.applications.push(app);
            logger.info(`Application ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, app.uid);
            break;
          case ChangeType.REMOVED:
            newData.applications = newData.applications.filter(a => a.uid !== app.uid);
            logger.info('Application removed:', app.uid);
            break;
        }
        break;
      }

      case SubscriptionType.APPLICATION_STATE: {
        const state = fromBinary(ApplicationStateSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.applicationStates.set(state.applicationUid, state);
            // Force applications array update to trigger UI recomputation
            newData.applications = [...newData.applications];
            logger.info(`Application state ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'} for:`, state.applicationUid, 'Status:', state.status);
            break;
          case ChangeType.REMOVED:
            newData.applicationStates.delete(state.applicationUid);
            newData.applications = [...newData.applications];
            logger.info('Application state removed for:', state.applicationUid);
            break;
        }
        break;
      }

      case SubscriptionType.APPLICATION_RESOURCE: {
        const resource = fromBinary(ApplicationResourceSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.applicationResources.set(resource.applicationUid, resource);
            // Force applications array update to trigger UI recomputation
            newData.applications = [...newData.applications];
            logger.info(`Application resource ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'} for:`, resource.applicationUid);
            break;
          case ChangeType.REMOVED:
            newData.applicationResources.delete(resource.applicationUid);
            newData.applications = [...newData.applications];
            logger.info('Application resource removed for:', resource.applicationUid);
            break;
        }
        break;
      }

      case SubscriptionType.LABEL: {
        const label = fromBinary(LabelSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.labels = newData.labels.filter(l => l.uid !== label.uid);
            newData.labels.push(label);
            logger.info(`Label ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, label.name);
            break;
          case ChangeType.REMOVED:
            newData.labels = newData.labels.filter(l => l.uid !== label.uid);
            logger.info('Label removed:', label.name);
            break;
        }
        break;
      }

      case SubscriptionType.NODE: {
        const node = fromBinary(NodeSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.nodes = newData.nodes.filter(n => n.uid !== node.uid);
            newData.nodes.push(node);
            logger.info(`Node ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, node.name);
            break;
          case ChangeType.REMOVED:
            newData.nodes = newData.nodes.filter(n => n.uid !== node.uid);
            logger.info('Node removed:', node.name);
            break;
        }
        break;
      }

      case SubscriptionType.USER: {
        const user = fromBinary(UserSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.users = newData.users.filter(u => u.name !== user.name);
            newData.users.push(user);
            logger.info(`User ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, user.name);
            break;
          case ChangeType.REMOVED:
            newData.users = newData.users.filter(u => u.name !== user.name);
            logger.info('User removed:', user.name);
            break;
        }
        break;
      }

      case SubscriptionType.ROLE: {
        const role = fromBinary(RoleSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.roles = newData.roles.filter(r => r.name !== role.name);
            newData.roles.push(role);
            logger.info(`Role ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, role.name);
            break;
          case ChangeType.REMOVED:
            newData.roles = newData.roles.filter(r => r.name !== role.name);
            logger.info('Role removed:', role.name);
            break;
        }
        break;
      }

      case SubscriptionType.USER_GROUP: {
        const usergroup = fromBinary(UserGroupSchema, binaryData);
        switch (response.changeType) {
          case ChangeType.CREATED:
          case ChangeType.UPDATED:
            newData.usergroups = newData.usergroups.filter(g => g.name !== usergroup.name);
            newData.usergroups.push(usergroup);
            logger.info(`User group ${response.changeType === ChangeType.CREATED ? 'created' : 'updated'}:`, usergroup.name);
            break;
          case ChangeType.REMOVED:
            newData.usergroups = newData.usergroups.filter(g => g.name !== usergroup.name);
            logger.info('User group removed:', usergroup.name);
            break;
        }
        break;
      }
    }
  } catch (err) {
    logger.error('Error processing subscription update:', err);
    throw err;
  }

  return newData;
}

