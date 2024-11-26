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

package msg

import (
	"encoding/json"
	"hash/crc32"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Basic container with the message to transfer type of the request
// Works in 2 parts: First contains service data and then in Data the rest of it which is not
// json-decoded right away. It should save some processing and help to skip decode of repeated msg
type Message struct {
	Type string // Type of the request, primary way to determine the transferred data
	Resp string // Responce on a specific request type, will be identical to the Req field

	// Actual data, for types it's array, for other ones could be anything
	Data util.UnparsedJSON
	// Used to verify data and quickly drop the message if already applied
	Sum uint32
}

// Creates new message and calculates checksum out of it
func NewMessage(type_name, resp string, data any) *Message {
	d, err := json.Marshal(data)
	if err != nil {
		log.Error("Unable to marshal json data:", err)
		return nil
	}

	return &Message{
		Type: type_name,
		Resp: resp,
		Data: util.UnparsedJSON(d),
		Sum:  crc32.Checksum(d, crc32.IEEETable),
	}
}

// Message Sync data allows to get the cluster data from the last sync point
type Sync struct {
	From time.Time
}
