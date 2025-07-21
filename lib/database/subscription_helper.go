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

package database

import (
	"github.com/adobe/aquarium-fish/lib/log"
)

// subscribeHelper adds a channel to a subscription list (generic helper)
func subscribeHelper[T any](d *Database, channels *[]chan T, ch chan T) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	*channels = append(*channels, ch)
}

// unsubscribeHelper removes a channel from a subscription list (generic helper)
func unsubscribeHelper[T any](d *Database, channels *[]chan T, ch chan T) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	for i, existing := range *channels {
		if existing == ch {
			// Remove channel from slice
			*channels = append((*channels)[:i], (*channels)[i+1:]...)
			break
		}
	}
}

// notifySubscribersHelper sends notifications to all subscribers (generic helper)
func notifySubscribersHelper[T any](d *Database, channels *[]chan T, event T, eventType string) {
	// Make a copy of channels while holding the lock
	d.subsMu.RLock()
	channelsCopy := make([]chan T, len(*channels))
	copy(channelsCopy, *channels)
	d.subsMu.RUnlock()

	// Send notifications without holding the lock
	go func() {
		for _, ch := range channelsCopy {
			// Use select with default to prevent panic if channel is closed
			select {
			case ch <- event:
				// Successfully sent notification
			default:
				// Channel is closed or full, skip this subscriber
				log.WithFunc("database", "notifySubscribersHelper").Debug("Failed to send notification, channel closed or full", "event_type", eventType)
			}
		}
	}()
}
