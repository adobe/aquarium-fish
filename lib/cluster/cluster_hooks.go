/**
 * Copyright 2023 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package cluster

import (
	"reflect"

	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/log"
)

// Hook for processing of create or update in database
// Used to connect cluster to DB operations to trigger the cluster automatic sync system
func (cl *Cluster) HookCreateUpdate(db *gorm.DB) {
	// Mostly borrowed from gorm src
	if db.Error != nil || db.Statement.Schema == nil || db.Statement.SkipHooks {
		return
	}

	switch db.Statement.ReflectValue.Kind() {
	case reflect.Slice, reflect.Array:
		db.Statement.CurDestIndex = 0
		for i := 0; i < db.Statement.ReflectValue.Len(); i++ {
			if value := reflect.Indirect(db.Statement.ReflectValue.Index(i)); value.CanAddr() {
				if _, ok := cl.ImportTypeAllowed(value.Type().Name()); ok {
					log.Debug("GORM create/update:", value.Type().Name())
					cl.Send(msg.NewMessage(value.Type().Name(), "", []any{value.Addr().Interface()}))
				}
			}
			db.Statement.CurDestIndex++
		}
	case reflect.Struct:
		if db.Statement.ReflectValue.CanAddr() {
			if _, ok := cl.ImportTypeAllowed(db.Statement.ReflectValue.Type().Name()); ok {
				log.Debug("GORM create/update:", db.Statement.ReflectValue.Type().Name())
				cl.Send(msg.NewMessage(db.Statement.ReflectValue.Type().Name(), "", []any{db.Statement.ReflectValue.Addr().Interface()}))
			}
		}
	}
}
