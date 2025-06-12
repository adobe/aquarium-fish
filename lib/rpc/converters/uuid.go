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

package converters

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// StringToApplicationUID converts a string to ApplicationUID
func StringToApplicationUID(s string) (types.ApplicationUID, error) {
	uid, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid ApplicationUID: %w", err)
	}
	return uid, nil
}

// StringToLabelUID converts a string to LabelUID
func StringToLabelUID(s string) (types.LabelUID, error) {
	uid, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid LabelUID: %w", err)
	}
	return uid, nil
}

// StringToNodeUID converts a string to NodeUID
func StringToNodeUID(s string) (types.NodeUID, error) {
	uid, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid NodeUID: %w", err)
	}
	return uid, nil
}

// StringToApplicationTaskUID converts a string to ApplicationTaskUID
func StringToApplicationTaskUID(s string) (types.ApplicationTaskUID, error) {
	uid, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid ApplicationTaskUID: %w", err)
	}
	return uid, nil
}

// UnparsedJSONToStruct converts UnparsedJSON to structpb.Struct
func UnparsedJSONToStruct(j util.UnparsedJSON) (*structpb.Struct, error) {
	if len(j) == 0 {
		return &structpb.Struct{}, nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(j), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal UnparsedJSON: %w", err)
	}

	s, err := structpb.NewStruct(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to structpb.Struct: %w", err)
	}

	return s, nil
}

// StructToUnparsedJSON converts structpb.Struct to UnparsedJSON
func StructToUnparsedJSON(s *structpb.Struct) (util.UnparsedJSON, error) {
	if s == nil {
		return "{}", nil
	}

	data := s.AsMap()
	bytes, err := json.Marshal(data)
	if err != nil {
		return "{}", fmt.Errorf("failed to marshal structpb.Struct: %w", err)
	}

	return util.UnparsedJSON(bytes), nil
}
