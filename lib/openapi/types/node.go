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

package types

import (
	"fmt"
	"time"
)

const NODE_PING_DELAY = 30

var NodePingDuplicationErr = fmt.Errorf("Fish Node: Unable to join the Aquarium cluster due to " +
	"the node with the same name pinged the cluster less then 2xNODE_PING_DELAY time ago")

func (n *Node) Init() error {
	curr_time := time.Now()

	// Allow to join cluster only if it's the new node or the existing node
	// was here 2xNODE_PING_DELAY seconds ago to prevent duplication
	if curr_time.Before(n.UpdatedAt.Add(NODE_PING_DELAY * 2 * time.Second)) {
		return NodePingDuplicationErr
	}

	// Collect the node definition data
	n.Definition.Update()

	return nil
}
