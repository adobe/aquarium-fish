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

package main

import (
	"bytes"
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	protogen.Options{}.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL) | uint64(pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
		plugin.SupportedEditionsMinimum = descriptorpb.Edition_EDITION_PROTO2
		plugin.SupportedEditionsMaximum = descriptorpb.Edition_EDITION_2023

		var buf bytes.Buffer
		buf.WriteString(`/**
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
`)

		buf.WriteString("\npackage database\n\n")
		buf.WriteString("// ObjectTypes - list of all object types in the system\n")
		buf.WriteString("const (\n")

		seenMessages := make(map[string]bool)

		for _, f := range plugin.Files {
			if !f.Generate {
				continue
			}

			for _, message := range f.Messages {
				messageName := string(message.Desc.Name())
				if strings.HasSuffix(messageName, "Request") || strings.HasSuffix(messageName, "Response") {
					continue
				}

				// Skip if we've already seen this message type
				if seenMessages[messageName] {
					continue
				}
				seenMessages[messageName] = true

				// Convert PascalCase to snake_case
				constName := fmt.Sprintf("Object%s", messageName)
				buf.WriteString(fmt.Sprintf("\t%s = %q\n", constName, messageName))
			}
		}

		buf.WriteString(")\n")

		outputFile := "object_list.gen.go"
		genFile := plugin.NewGeneratedFile(outputFile, "")
		_, err := genFile.Write(buf.Bytes())
		return err
	})
}
