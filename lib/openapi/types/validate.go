/**
 * Copyright 2024 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package types

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/util"
)

func (a *Application) Validate() error {
	if a.LabelUID == uuid.Nil {
		return fmt.Errorf("Types: LabelUID can't be unset")
	}
	if a.Metadata == "" {
		a.Metadata = "{}"
	}
	return nil
}

func (as *ApplicationState) Validate() error {
	if as.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Types: ApplicationUID can't be unset")
	}
	if as.Status == "" {
		return fmt.Errorf("Types: Status can't be empty")
	}
	return nil
}

func (at *ApplicationTask) Validate() error {
	if at.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Types: ApplicationUID can't be unset")
	}
	if at.Task == "" {
		return fmt.Errorf("Types: Task can't be empty")
	}
	if at.Options == "" {
		at.Options = util.UnparsedJSON("{}")
	}
	if at.Result == "" {
		at.Result = util.UnparsedJSON("{}")
	}
	return nil
}

func (l *Label) Validate() error {
	if l.Name == "" {
		return fmt.Errorf("Types: Name can't be empty")
	}
	for i, def := range l.Definitions {
		if def.Driver == "" {
			return fmt.Errorf("Types: Driver can't be empty in Label Definition %d", i)
		}
		if def.Resources.Cpu < 1 {
			return fmt.Errorf("Types: Resources CPU can't be less than 1 in Label Definition %d", i)
		}
		if def.Resources.Ram < 1 {
			return fmt.Errorf("Types: Resources RAM can't be less than 1 in Label Definition %d", i)
		}
		_, err := time.ParseDuration(def.Resources.Lifetime)
		if def.Resources.Lifetime != "" && err != nil {
			return fmt.Errorf("Types: Resources Lifetime parse error in Label Definition %d: %v", i, err)
		}
		if def.Options == "" {
			l.Definitions[i].Options = "{}"
		}
	}
	if l.Metadata == "" {
		l.Metadata = "{}"
	}
	return nil
}

func (l *Location) Validate() error {
	if l.Name == "" {
		return fmt.Errorf("Types: Name can't be empty")
	}
	return nil
}

func (n *Node) Validate() error {
	if n.Name == "" {
		return fmt.Errorf("Types: Name can't be empty")
	}
	if n.Pubkey == nil {
		return fmt.Errorf("Types: Node should be initialized before create")
	}
	return nil
}

func (r *Resource) Validate() error {
	if r.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Types: ApplicationUID can't be unset")
	}
	if r.LabelUID == uuid.Nil {
		return fmt.Errorf("Types: LabelUID can't be unset")
	}
	if r.NodeUID == uuid.Nil {
		return fmt.Errorf("Types: NodeUID can't be unset")
	}
	if len(r.Identifier) == 0 {
		return fmt.Errorf("Types: Identifier can't be empty")
	}
	// TODO: check JSON
	if len(r.Metadata) < 2 {
		return fmt.Errorf("Types: Metadata can't be empty")
	}
	return nil
}

func (sm *ServiceMapping) Validate() error {
	if sm.Service == "" {
		return fmt.Errorf("Types: Service can't be empty")
	}
	if sm.Redirect == "" {
		return fmt.Errorf("Types: Redirect can't be empty")
	}
	return nil
}

func (u *User) Validate() error {
	if u.Name == "" {
		return fmt.Errorf("Types: Name can't be empty")
	}
	if u.Hash.IsEmpty() {
		return fmt.Errorf("Types: Hash can't be empty")
	}
	return nil
}

func (v *Vote) Validate() error {
	if v.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Types: ApplicationUID can't be unset")
	}
	if v.NodeUID == uuid.Nil {
		return fmt.Errorf("Types: NodeUID can't be unset")
	}
	return nil
}
