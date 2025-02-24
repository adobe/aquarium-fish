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

package fish

import (
	"fmt"
	"strconv"
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// LabelFind returns list of Labels that fits filters
func (f *Fish) LabelList(filters types.LabelListGetParams) (labels []types.Label, err error) {
	err = f.db.Collection("label").List(&labels)
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

func (f *Fish) LabelListName(name string) (labels []types.Label, err error) {
	allLabels := []types.Label{}
	if err = f.db.Collection("label").List(&allLabels); err == nil {
		for _, l := range allLabels {
			if l.Name == name {
				labels = append(labels, l)
			}
		}
	}
	return labels, err
}

// LabelCreate makes new Label
func (f *Fish) LabelCreate(l *types.Label) error {
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
		if def.Resources.Cpu < 1 {
			return fmt.Errorf("Fish: Resources CPU can't be less than 1 in Label Definition %d", i)
		}
		if def.Resources.Ram < 1 {
			return fmt.Errorf("Fish: Resources RAM can't be less than 1 in Label Definition %d", i)
		}
		_, err := time.ParseDuration(def.Resources.Lifetime)
		if def.Resources.Lifetime != "" && err != nil {
			return fmt.Errorf("Fish: Resources Lifetime parse error in Label Definition %d: %v", i, err)
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
	founds, err := f.LabelList(types.LabelListGetParams{Name: &l.Name, Version: &strversion})
	if err != nil || len(founds) != 0 {
		return fmt.Errorf("Fish: Label name + version is not unique: %v", err)
	}

	l.UID = f.NewUID()
	l.CreatedAt = time.Now()
	return f.db.Collection("label").Add(l.UID.String(), l)
}

// Intentionally disabled - labels can be created once and can't be updated
// Create label with incremented version instead
/*func (f *Fish) LabelSave(label *types.Label) error {
	return f.db.Save(label).Error
}*/

// LabelGet returns Label by UID
func (f *Fish) LabelGet(uid types.LabelUID) (label *types.Label, err error) {
	err = f.db.Collection("label").Get(uid.String(), &label)
	return label, err
}

// LabelDelete deletes the Label by UID
func (f *Fish) LabelDelete(uid types.LabelUID) error {
	return f.db.Collection("label").Delete(uid.String())
}
