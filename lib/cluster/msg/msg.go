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

package msg

import (
	"time"

	"github.com/adobe/aquarium-fish/lib/util"
)

// Basic container with the message to transfer type of the request
type Message struct {
	Type string // Type of the request
	Resp string // Responce on a specific request type, will be identical to the Req field

	Data util.UnparsedJson
}

// Message Sync data allows to get the cluster data from the last sync point
type Sync struct {
	From time.Time
}
