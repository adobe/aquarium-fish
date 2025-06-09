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
)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
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

		for _, f := range gen.Files {
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
		genFile := gen.NewGeneratedFile(outputFile, "")
		_, err := genFile.Write(buf.Bytes())
		return err
	})
}
