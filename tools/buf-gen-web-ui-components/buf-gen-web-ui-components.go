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

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/adobe/aquarium-fish/lib/build"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
)

const (
	usage = "Aquarium WEB UI Components generator plugin.\n\nFlags:\n  -h, --help\tPrint this help and exit.\n      --version\tPrint the version and exit."
)

// TypeInfo represents information about a protobuf type for React component generation
type TypeInfo struct {
	Name        string
	TypeName    string
	Package     string
	Fields      []FieldInfo
	Enums       []EnumInfo
	IsTimestamp bool
	IsStruct    bool
	IsEnum      bool
	IsMessage   bool
	Comment     string
}

// FieldInfo represents information about a protobuf field
type FieldInfo struct {
	Name         string
	JSONName     string
	TypeName     string
	TypeScript   string
	IsOptional   bool
	IsRepeated   bool
	IsMessage    bool
	IsEnum       bool
	IsMap        bool
	IsTimestamp  bool
	IsStruct     bool
	Comment      string
	DefaultValue string
	MapKeyType   string
	MapValueType string
	// UI options
	NoCreate     bool
	NoEdit       bool
	DisplayName  string
	AutofillType string
	// Message type name for nested components
	MessageTypeName string
}

// EnumInfo represents information about a protobuf enum
type EnumInfo struct {
	Name    string
	Values  []EnumValue
	Comment string
}

// EnumValue represents a protobuf enum value
type EnumValue struct {
	Name    string
	Value   int32
	Comment string
}

// loadTemplates loads all template files from the templates directory
func loadTemplates() (*template.Template, error) {
	// Get the directory where this Go file is located
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("failed to get current file path")
	}

	toolDir := filepath.Dir(filename)
	templatesDir := filepath.Join(toolDir, "templates")

	// Load all template files
	pattern := filepath.Join(templatesDir, "*.tmpl")
	tmpl := template.New("react_component").Funcs(template.FuncMap{
		"lower":      strings.ToLower,
		"htmlEscape": template.HTMLEscapeString,
	})

	// Parse all template files
	tmpl, err := tmpl.ParseGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template files: %v", err)
	}

	return tmpl, nil
}

// getTypeScriptType converts protobuf field type to TypeScript type
func getTypeScriptType(field *protogen.Field) string {
	switch field.Desc.Kind() {
	case protoreflect.StringKind:
		return "string"
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.BytesKind:
		return "Uint8Array"
	case protoreflect.MessageKind:
		if field.Desc.Message().FullName() == "google.protobuf.Timestamp" {
			return "string" // We'll handle timestamps as datetime-local strings
		}
		if field.Desc.Message().FullName() == "google.protobuf.Struct" {
			return "Record<string, any>"
		}
		return "any" // For other message types
	case protoreflect.EnumKind:
		return "number"
	case protoreflect.GroupKind:
		return "any"
	default:
		return "any"
	}
}

// getDefaultValue returns the default value for a TypeScript type
func getDefaultValue(field *protogen.Field) string {
	if field.Desc.IsList() {
		return "[]"
	}
	if field.Desc.IsMap() {
		return "{}"
	}
	if field.Desc.HasOptionalKeyword() {
		return "undefined"
	}

	switch field.Desc.Kind() {
	case protoreflect.StringKind:
		return "''"
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind:
		return "0"
	case protoreflect.BoolKind:
		return "false"
	case protoreflect.BytesKind:
		return "new Uint8Array()"
	case protoreflect.MessageKind:
		if field.Desc.Message().FullName() == "google.protobuf.Timestamp" {
			return "''" // Empty string for datetime-local
		}
		if field.Desc.Message().FullName() == "google.protobuf.Struct" {
			return "{}"
		}
		return "null"
	case protoreflect.EnumKind:
		return "0"
	case protoreflect.GroupKind:
		return "null"
	default:
		return "null"
	}
}

// cleanComment removes proto comment markers and formats for display
func cleanComment(comment string) string {
	// Remove // at the beginning of lines
	lines := strings.Split(comment, "\n")
	var cleanedLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			line = strings.TrimSpace(line[2:])
		}
		if line != "" {
			cleanedLines = append(cleanedLines, line)
		}
	}
	return strings.Join(cleanedLines, " ")
}

// camelToTitle converts camelCase to Title Case
func camelToTitle(s string) string {
	if len(s) == 0 {
		return s
	}

	// Insert space before capital letters
	re := regexp.MustCompile("([a-z])([A-Z])")
	s = re.ReplaceAllString(s, "$1 $2")

	// Capitalize first letter
	return strings.ToUpper(s[:1]) + s[1:]
}

// processMessage processes a protobuf message and returns TypeInfo
func processMessage(msg *protogen.Message) *TypeInfo {
	// Get the proto file name without extension for the import path
	protoFileName := msg.Desc.ParentFile().Path()
	// Extract just the filename without the path
	if slashIndex := strings.LastIndex(protoFileName, "/"); slashIndex != -1 {
		protoFileName = protoFileName[slashIndex+1:]
	}
	protoFileName = strings.TrimSuffix(protoFileName, ".proto")

	typeInfo := &TypeInfo{
		Name:      msg.GoIdent.GoName,
		TypeName:  msg.GoIdent.GoName,
		Package:   strings.ReplaceAll(string(msg.Desc.ParentFile().Package()), ".", "/") + "/" + protoFileName,
		Comment:   cleanComment(msg.Comments.Leading.String()),
		IsMessage: true,
	}

	// Process fields
	for _, field := range msg.Fields {
		fieldInfo := FieldInfo{
			Name:         camelToTitle(field.GoName),
			JSONName:     field.Desc.JSONName(),
			TypeName:     string(field.Desc.Name()),
			TypeScript:   getTypeScriptType(field),
			IsOptional:   field.Desc.HasOptionalKeyword(),
			IsRepeated:   field.Desc.IsList(),
			IsMap:        field.Desc.IsMap(),
			Comment:      cleanComment(field.Comments.Leading.String()),
			DefaultValue: getDefaultValue(field),
		}

		// Check for UI options
		fieldOptions, ok := field.Desc.Options().(*descriptorpb.FieldOptions)
		if ok && fieldOptions != nil {
			if fieldUIConfig := proto.GetExtension(fieldOptions, aquariumv2.E_FieldUiConfig); fieldUIConfig != nil {
				if config, ok := fieldUIConfig.(*aquariumv2.FieldUiConfig); ok && config != nil {
					if config.Nocreate != nil && config.GetNocreate() {
						fieldInfo.NoCreate = true
					}
					if config.Noedit != nil && config.GetNoedit() {
						fieldInfo.NoEdit = true
					}
					if config.Name != nil && config.GetName() != "" {
						fieldInfo.DisplayName = config.GetName()
					}
					if config.Autofill != nil && config.GetAutofill() != "" {
						fieldInfo.AutofillType = config.GetAutofill()
					}
					if config.Required != nil {
						fieldInfo.IsOptional = !config.GetRequired()
					}
				}
			}
		}

		// If no custom display name, use the field name
		if fieldInfo.DisplayName == "" {
			fieldInfo.DisplayName = fieldInfo.Name
		}

		// Check for special types
		if field.Desc.Kind() == protoreflect.MessageKind {
			fieldInfo.IsMessage = true
			if field.Desc.Message().FullName() == "google.protobuf.Timestamp" {
				fieldInfo.IsTimestamp = true
			} else if field.Desc.Message().FullName() == "google.protobuf.Struct" {
				fieldInfo.IsStruct = true
			} else {
				// Set the message type name for nested components
				fieldInfo.MessageTypeName = string(field.Desc.Message().Name())
			}
		} else if field.Desc.Kind() == protoreflect.EnumKind {
			fieldInfo.IsEnum = true
		}

		// Process map types
		if field.Desc.IsMap() {
			mapKey := field.Desc.MapKey()
			mapValue := field.Desc.MapValue()

			// Set key type
			switch mapKey.Kind() {
			case protoreflect.StringKind:
				fieldInfo.MapKeyType = "string"
			case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
				protoreflect.Sint64Kind, protoreflect.Uint64Kind, protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind,
				protoreflect.FloatKind, protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind, protoreflect.DoubleKind:
				fieldInfo.MapKeyType = "number"
			case protoreflect.BoolKind, protoreflect.EnumKind, protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
				fieldInfo.MapKeyType = "string"
			default:
				fieldInfo.MapKeyType = "string"
			}

			// Set value type
			if mapValue.Kind() == protoreflect.MessageKind {
				fieldInfo.MapValueType = string(mapValue.Message().Name())
				fieldInfo.IsMessage = true
			} else {
				switch mapValue.Kind() {
				case protoreflect.StringKind:
					fieldInfo.MapValueType = "string"
				case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Uint32Kind, protoreflect.Uint64Kind,
					protoreflect.Sint32Kind, protoreflect.Sint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind,
					protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind:
					fieldInfo.MapValueType = "number"
				case protoreflect.BoolKind:
					fieldInfo.MapValueType = "boolean"
				case protoreflect.EnumKind, protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
					fieldInfo.MapValueType = "any"
				default:
					fieldInfo.MapValueType = "any"
				}
			}
		}

		typeInfo.Fields = append(typeInfo.Fields, fieldInfo)
	}

	return typeInfo
}

// shouldGenerateComponent determines if we should generate a component for this message
func shouldGenerateComponent(msg *protogen.Message) bool {
	// Check if message has UI options
	msgOptions, ok := msg.Desc.Options().(*descriptorpb.MessageOptions)
	if ok && msgOptions != nil {
		if uiConfig := proto.GetExtension(msgOptions, aquariumv2.E_UiConfig); uiConfig != nil {
			if config, ok := uiConfig.(*aquariumv2.UiConfig); ok && config != nil {
				if config.GenerateUi != nil && config.GetGenerateUi() {
					return true
				}
			}
		}
	}

	// Also check if any field has UI options
	for _, field := range msg.Fields {
		fieldOptions, ok := field.Desc.Options().(*descriptorpb.FieldOptions)
		if ok && fieldOptions != nil {
			if fieldUIConfig := proto.GetExtension(fieldOptions, aquariumv2.E_FieldUiConfig); fieldUIConfig != nil {
				if config, ok := fieldUIConfig.(*aquariumv2.FieldUiConfig); ok && config != nil {
					return true
				}
			}
		}
	}

	return false
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Fprintln(os.Stdout, build.Version)
		os.Exit(0)
	}
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Fprintln(os.Stdout, usage)
		os.Exit(0)
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	protogen.Options{}.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL) | uint64(pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
		plugin.SupportedEditionsMinimum = descriptorpb.Edition_EDITION_PROTO2
		plugin.SupportedEditionsMaximum = descriptorpb.Edition_EDITION_2023

		var components []TypeInfo
		componentDir := "components"

		for _, f := range plugin.Files {
			if !f.Generate {
				continue
			}

			for _, message := range f.Messages {
				if !shouldGenerateComponent(message) {
					continue
				}

				typeInfo := processMessage(message)
				components = append(components, *typeInfo)

				// Load templates
				tmpl, err := loadTemplates()
				if err != nil {
					return fmt.Errorf("Error loading templates: %v", err)
				}

				// Generate React component file
				var buf bytes.Buffer
				err = tmpl.ExecuteTemplate(&buf, "main-component", typeInfo)
				if err != nil {
					return fmt.Errorf("Error executing template for %s: %v", typeInfo.Name, err)
				}

				// Write component file
				componentFile := filepath.Join(componentDir, fmt.Sprintf("%sForm.tsx", typeInfo.Name))
				genFile := plugin.NewGeneratedFile(componentFile, "")
				if _, err := genFile.Write(buf.Bytes()); err != nil {
					return fmt.Errorf("Error writing component file %s: %v", componentFile, err)
				}
			}
		}

		// Generate index file
		if len(components) > 0 {
			sort.Slice(components, func(i, j int) bool {
				return components[i].Name < components[j].Name
			})

			// Load templates for index generation
			tmpl, err := loadTemplates()
			if err != nil {
				return fmt.Errorf("Error loading templates: %v", err)
			}

			var buf bytes.Buffer
			err = tmpl.ExecuteTemplate(&buf, "index-template", map[string]any{
				"Components": components,
			})
			if err != nil {
				return fmt.Errorf("Error executing index template: %v", err)
			}

			indexFile := filepath.Join(componentDir, "index.ts")
			genFile := plugin.NewGeneratedFile(indexFile, "")
			if _, err := genFile.Write(buf.Bytes()); err != nil {
				return fmt.Errorf("Error writing index file: %v", err)
			}
		}

		return nil
	})
}
