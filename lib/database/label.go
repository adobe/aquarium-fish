/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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
	"context"
	"fmt"
	"strconv"
	"time"

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func (d *Database) subscribeLabelImpl(_ context.Context, ch chan LabelSubscriptionEvent) {
	subscribeHelper(d, &d.subsLabel, ch)
}

// unsubscribeLabelImpl removes a channel from the subscription list
func (d *Database) unsubscribeLabelImpl(_ context.Context, ch chan LabelSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsLabel, ch)
}

// LabelListParams reflection of RPC LabelServiceListRequest to pass to database function LabelList
type LabelListParams struct {
	Name    *string
	Version *string
}

// labelListImpl returns list of Labels that fits filters
func (d *Database) labelListImpl(_ context.Context, filters LabelListParams) (labels []typesv2.Label, err error) {
	d.beMu.RLock()
	err = d.be.Collection(ObjectLabel).List(&labels)
	d.beMu.RUnlock()

	var filterVersion int32
	if filters.Version != nil && *filters.Version != "last" {
		// Try to convert to int64
		version64, err := strconv.ParseInt(*filters.Version, 10, 32)
		if err != nil {
			return labels, fmt.Errorf("unable to parse Version integer: %v", err)
		}
		// Converting to int32
		filterVersion = int32(version64)
	}
	if err == nil && (filters.Name != nil || filters.Version != nil) {
		passed := []typesv2.Label{}
		uniqueLabels := make(map[string]typesv2.Label)
		for _, label := range labels {
			if filters.Name != nil && label.Name != *filters.Name {
				continue
			}
			if filters.Version != nil {
				if *filters.Version == "last" {
					if item, ok := uniqueLabels[label.Name]; !ok || item.Version < label.Version {
						uniqueLabels[label.Name] = label
					}
					continue
				}
				// Filtering specific version
				if label.Version != filterVersion {
					continue
				}
			}
			passed = append(passed, label)
		}
		if filters.Version != nil && *filters.Version == "last" {
			for _, label := range uniqueLabels {
				passed = append(passed, label)
			}
			labels = passed
		}
		labels = passed
	}
	return labels, err
}

// labelCreateImpl makes new Label
func (d *Database) labelCreateImpl(ctx context.Context, l *typesv2.Label) error {
	if l.Name == "" {
		return fmt.Errorf("label name can't be empty")
	}
	if l.Version < 0 {
		return fmt.Errorf("label version can't be less then 0")
	}
	for i, def := range l.Definitions {
		if def.Driver == "" {
			return fmt.Errorf("label definition driver can't be empty in Label Definition %d", i)
		}
		// Check the provided images
		for index := range l.Definitions[i].Images {
			if err := l.Definitions[i].Images[index].Validate(); err != nil {
				return fmt.Errorf("label definition images validation failed: %v", err)
			}
		}
		// Executing Validate here on the list to allow to modify the incorrect data
		if err := l.Definitions[i].Resources.Validate([]string{}, false); err != nil {
			return fmt.Errorf("label definition resources validation failed: %v", err)
		}
	}

	// Name and version need to be unique
	strversion := fmt.Sprintf("%d", l.Version)
	founds, err := d.LabelList(ctx, LabelListParams{Name: &l.Name, Version: &strversion})
	if err != nil || len(founds) != 0 {
		return fmt.Errorf("label name + version is not unique: %v", err)
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	l.Uid = d.NewUID()
	l.CreatedAt = time.Now()
	l.UpdatedAt = l.CreatedAt
	err = d.be.Collection(ObjectLabel).Add(l.Uid.String(), l)

	if err == nil {
		// Notify subscribers about the new Label
		notifySubscribersHelper(d, &d.subsLabel, NewCreateEvent(l), ObjectLabel)
	}

	return err
}

// labelSaveImpl allows to save label with version=0, others are immutable
func (d *Database) labelSaveImpl(_ context.Context, l *typesv2.Label) error {
	if l.Version != 0 {
		return fmt.Errorf("unable to save label with version != 0")
	}
	l.UpdatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectLabel).Add(l.Uid.String(), l)
	d.beMu.RUnlock()

	if err == nil {
		// Notify subscribers about the updated User
		notifySubscribersHelper(d, &d.subsLabel, NewUpdateEvent(l), ObjectLabel)
	}

	return err
}

// labelGetImpl returns Label by UID
func (d *Database) labelGetImpl(_ context.Context, uid typesv2.LabelUID) (label *typesv2.Label, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectLabel).Get(uid.String(), &label)
	return label, err
}

// labelDeleteImpl deletes the Label by UID
func (d *Database) labelDeleteImpl(ctx context.Context, uid typesv2.LabelUID) error {
	// Get the object before deleting it for notification
	l, getErr := d.LabelGet(ctx, uid)
	if getErr != nil {
		return getErr
	}

	d.beMu.RLock()
	err := d.be.Collection(ObjectLabel).Delete(uid.String())
	d.beMu.RUnlock()

	if err == nil && l != nil {
		// Notify subscribers about the removed Label
		notifySubscribersHelper(d, &d.subsLabel, NewRemoveEvent(l), ObjectLabel)
	}

	return err
}
