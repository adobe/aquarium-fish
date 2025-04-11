/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package db

import (
	"fmt"
	"strconv"
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// LabelFind returns list of Labels that fits filters
func (d *Database) LabelList(filters types.LabelListGetParams) (labels []types.Label, err error) {
	err = d.be.Collection("label").List(&labels)
	filterVersion := 0
	if filters.Version != nil && *filters.Version != "last" {
		// Try to convert to int and if fails
		if filterVersion, err = strconv.Atoi(*filters.Version); err != nil {
			return labels, fmt.Errorf("Unable to parse Version integer: %v", err)
		}
	}
	if err == nil && (filters.Name != nil || filters.Version != nil) {
		passed := []types.Label{}
		uniqueLabels := make(map[string]types.Label)
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

func (d *Database) LabelListName(name string) (labels []types.Label, err error) {
	allLabels := []types.Label{}
	if err = d.be.Collection("label").List(&allLabels); err == nil {
		for _, l := range allLabels {
			if l.Name == name {
				labels = append(labels, l)
			}
		}
	}
	return labels, err
}

// LabelCreate makes new Label
func (d *Database) LabelCreate(l *types.Label) error {
	if l.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if l.Version < 1 {
		return fmt.Errorf("Fish: Version can't be less then 1")
	}
	for i, def := range l.Definitions {
		if def.Driver == "" {
			return fmt.Errorf("Fish: Driver can't be empty in Label Definition %d", i)
		}
		// Executing Validate here on the list to allow to modify the incorrect data
		if err := l.Definitions[i].Resources.Validate([]string{}, false); err != nil {
			return fmt.Errorf("Fish: Resources validation failed: %v", err)
		}
		if def.Options == "" {
			l.Definitions[i].Options = "{}"
		}
	}
	if l.Metadata == "" {
		l.Metadata = "{}"
	}

	// Name and version need to be unique
	strversion := fmt.Sprintf("%d", l.Version)
	founds, err := d.LabelList(types.LabelListGetParams{Name: &l.Name, Version: &strversion})
	if err != nil || len(founds) != 0 {
		return fmt.Errorf("Fish: Label name + version is not unique: %v", err)
	}

	l.UID = d.NewUID()
	l.CreatedAt = time.Now()
	return d.be.Collection("label").Add(l.UID.String(), l)
}

// Intentionally disabled - labels can be created once and can't be updated
// Create label with incremented version instead
/*func (d *Database) LabelSave(label *types.Label) error {
	return d.be.Save(label).Error
}*/

// LabelGet returns Label by UID
func (d *Database) LabelGet(uid types.LabelUID) (label *types.Label, err error) {
	err = d.be.Collection("label").Get(uid.String(), &label)
	return label, err
}

// LabelDelete deletes the Label by UID
func (d *Database) LabelDelete(uid types.LabelUID) error {
	return d.be.Collection("label").Delete(uid.String())
}
