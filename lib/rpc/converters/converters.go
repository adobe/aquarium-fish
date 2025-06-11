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

package converters

import (
	"fmt"
	"strconv"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
)

// ConvertRole converts types.Role to aquariumv2.Role
func ConvertRole(role *types.Role) *aquariumv2.Role {
	if role == nil {
		return nil
	}

	protoRole := &aquariumv2.Role{
		Name:        role.Name,
		CreatedAt:   timestamppb.New(role.CreatedAt),
		UpdatedAt:   timestamppb.New(role.UpdatedAt),
		Permissions: make([]*aquariumv2.Permission, len(role.Permissions)),
	}

	for i, p := range role.Permissions {
		protoRole.Permissions[i] = &aquariumv2.Permission{
			Resource: p.Resource,
			Action:   p.Action,
		}
	}

	return protoRole
}

// ConvertLabel converts types.Label to aquariumv2.Label
func ConvertLabel(label *types.Label) *aquariumv2.Label {
	if label == nil {
		return nil
	}

	metadata, err := structpb.NewStruct(map[string]any{
		"metadata": string(label.Metadata),
	})
	if err != nil {
		metadata = &structpb.Struct{}
	}

	protoLabel := &aquariumv2.Label{
		Uid:       label.UID.String(),
		Name:      label.Name,
		Version:   int32(label.Version),
		CreatedAt: timestamppb.New(label.CreatedAt),
		Metadata:  metadata,
	}

	protoLabel.Definitions = make([]*aquariumv2.LabelDefinition, len(label.Definitions))
	for i, def := range label.Definitions {
		protoLabel.Definitions[i] = convertLabelDefinition(&def)
	}

	return protoLabel
}

// ConvertLabelFromProto converts new aquariumv2.Label to types.Label
func ConvertLabelNewFromProto(label *aquariumv2.Label) (*types.Label, error) {
	if label == nil {
		return nil, fmt.Errorf("nil source label")
	}

	metadata, err := StructToUnparsedJSON(label.GetMetadata())
	if err != nil {
		return nil, fmt.Errorf("invalid metadata: %w", err)
	}

	outLabel := &types.Label{
		Name:      label.GetName(),
		Version:   int(label.GetVersion()),
		CreatedAt: label.GetCreatedAt().AsTime(),
		Metadata:  metadata,
	}

	// Convert definitions
	outLabel.Definitions = make(types.LabelDefinitions, len(label.GetDefinitions()))
	for i, def := range label.GetDefinitions() {
		if def == nil {
			continue
		}

		converted, err := convertLabelDefinitionFromProto(def)
		if err != nil {
			return nil, fmt.Errorf("failed to convert definition at index %d: %w", i, err)
		}
		outLabel.Definitions[i] = converted
	}

	return outLabel, nil
}

// ConvertLabelFromProto converts aquariumv2.Label to types.Label
func ConvertLabelFromProto(label *aquariumv2.Label) (*types.Label, error) {
	outLabel, err := ConvertLabelNewFromProto(label)
	if err != nil {
		return nil, err
	}

	outLabel.UID, err = StringToLabelUID(label.GetUid())
	if err != nil {
		return nil, err
	}

	return outLabel, nil
}

// ConvertNode converts types.Node to aquariumv2.Node
func ConvertNode(node *types.Node) *aquariumv2.Node {
	if node == nil {
		return nil
	}

	protoNode := &aquariumv2.Node{
		Uid:       node.UID.String(),
		Name:      node.Name,
		Address:   node.Address,
		Location:  node.Location,
		CreatedAt: timestamppb.New(node.CreatedAt),
		UpdatedAt: timestamppb.New(node.UpdatedAt),
	}

	if node.Pubkey != nil {
		protoNode.Pubkey = *node.Pubkey
	}

	// Convert NodeDefinition
	protoNode.Definition = convertNodeDefinition(&node.Definition)

	return protoNode
}

// ConvertApplication converts types.Application to aquariumv2.Application
func ConvertApplication(app *types.Application) *aquariumv2.Application {
	if app == nil {
		return nil
	}

	metadata, err := UnparsedJSONToStruct(app.Metadata)
	if err != nil {
		metadata = &structpb.Struct{}
	}

	return &aquariumv2.Application{
		Uid:       app.UID.String(),
		LabelUid:  app.LabelUID.String(),
		OwnerName: app.OwnerName,
		CreatedAt: timestamppb.New(app.CreatedAt),
		Metadata:  metadata,
	}
}

// ConvertApplicationNewFromProto converts aquariumv2.Application with no UID to types.Application
func ConvertApplicationNewFromProto(app *aquariumv2.Application) (*types.Application, error) {
	if app == nil {
		return nil, fmt.Errorf("nil source application")
	}

	labelUID, err := StringToLabelUID(app.GetLabelUid())
	if err != nil {
		return nil, err
	}

	metadata, err := StructToUnparsedJSON(app.GetMetadata())
	if err != nil {
		return nil, err
	}

	return &types.Application{
		LabelUID:  labelUID,
		OwnerName: app.GetOwnerName(),
		CreatedAt: app.GetCreatedAt().AsTime(),
		Metadata:  metadata,
	}, nil
}

// ConvertApplicationFromProto converts aquariumv2.Application to types.Application
func ConvertApplicationFromProto(app *aquariumv2.Application) (*types.Application, error) {
	outApp, err := ConvertApplicationNewFromProto(app)
	if err != nil {
		return nil, err
	}

	outApp.UID, err = StringToApplicationUID(app.GetUid())
	if err != nil {
		return nil, err
	}

	return outApp, nil
}

// ConvertApplicationState converts types.ApplicationState to aquariumv2.ApplicationState
func ConvertApplicationState(state *types.ApplicationState) *aquariumv2.ApplicationState {
	if state == nil {
		return nil
	}

	return &aquariumv2.ApplicationState{
		Uid:            state.UID.String(),
		ApplicationUid: state.ApplicationUID.String(),
		Status:         string(state.Status),
		Description:    state.Description,
		CreatedAt:      timestamppb.New(state.CreatedAt),
	}
}

// ConvertApplicationTask converts types.ApplicationTask to aquariumv2.ApplicationTask
func ConvertApplicationTask(task *types.ApplicationTask) *aquariumv2.ApplicationTask {
	if task == nil {
		return nil
	}

	options, err := UnparsedJSONToStruct(task.Options)
	if err != nil {
		options = &structpb.Struct{}
	}

	result, err := UnparsedJSONToStruct(task.Result)
	if err != nil {
		result = &structpb.Struct{}
	}

	return &aquariumv2.ApplicationTask{
		Uid:            task.UID.String(),
		ApplicationUid: task.ApplicationUID.String(),
		Task:           task.Task,
		Options:        options,
		Result:         result,
		When:           string(task.When),
		CreatedAt:      timestamppb.New(task.CreatedAt),
		UpdatedAt:      timestamppb.New(task.UpdatedAt),
	}
}

// ConvertApplicationTaskFromProto converts aquariumv2.ApplicationTask to types.ApplicationTask
func ConvertApplicationTaskFromProto(task *aquariumv2.ApplicationTask) (*types.ApplicationTask, error) {
	if task == nil {
		return nil, nil
	}

	uid, err := StringToApplicationTaskUID(task.GetUid())
	if err != nil {
		return nil, err
	}

	appUID, err := StringToApplicationUID(task.GetApplicationUid())
	if err != nil {
		return nil, err
	}

	options, err := StructToUnparsedJSON(task.GetOptions())
	if err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	result, err := StructToUnparsedJSON(task.GetResult())
	if err != nil {
		return nil, fmt.Errorf("invalid result: %w", err)
	}

	return &types.ApplicationTask{
		UID:            uid,
		ApplicationUID: appUID,
		Task:           task.GetTask(),
		Options:        options,
		Result:         result,
		When:           types.ApplicationStatus(task.GetWhen()),
		CreatedAt:      task.GetCreatedAt().AsTime(),
		UpdatedAt:      task.GetUpdatedAt().AsTime(),
	}, nil
}

// ConvertApplicationResource converts types.ApplicationResource to aquariumv2.ApplicationResource
func ConvertApplicationResource(res *types.ApplicationResource) *aquariumv2.ApplicationResource {
	if res == nil {
		return nil
	}

	metadata, err := structpb.NewStruct(map[string]any{
		"metadata": string(res.Metadata),
	})
	if err != nil {
		metadata = &structpb.Struct{}
	}

	protoRes := &aquariumv2.ApplicationResource{
		Uid:             res.UID.String(),
		ApplicationUid:  res.ApplicationUID.String(),
		LabelUid:        res.LabelUID.String(),
		NodeUid:         res.NodeUID.String(),
		DefinitionIndex: int32(res.DefinitionIndex),
		Identifier:      res.Identifier,
		HwAddr:          res.HwAddr,
		IpAddr:          res.IpAddr,
		CreatedAt:       timestamppb.New(res.CreatedAt),
		UpdatedAt:       timestamppb.New(res.UpdatedAt),
		Metadata:        metadata,
	}

	if res.Authentication != nil {
		protoRes.Authentication = ConvertAuthentication(res.Authentication)
	}

	if res.Timeout != nil {
		protoRes.Timeout = timestamppb.New(*res.Timeout)
	}

	return protoRes
}

// ConvertAuthentication converts types.Authentication to aquariumv2.Authentication
func ConvertAuthentication(auth *types.Authentication) *aquariumv2.Authentication {
	if auth == nil {
		return nil
	}

	return &aquariumv2.Authentication{
		Username: auth.Username,
		Password: auth.Password,
		Key:      auth.Key,
		Port:     int32(auth.Port),
	}
}

// Helper functions

func convertLabelDefinition(def *types.LabelDefinition) *aquariumv2.LabelDefinition {
	if def == nil {
		return nil
	}

	options, err := structpb.NewStruct(map[string]any{
		"options": string(def.Options),
	})
	if err != nil {
		options = &structpb.Struct{}
	}

	protoDef := &aquariumv2.LabelDefinition{
		Driver:  def.Driver,
		Options: options,
	}

	if def.Authentication != nil {
		protoDef.Authentication = &aquariumv2.Authentication{
			Username: def.Authentication.Username,
			Password: def.Authentication.Password,
			Key:      def.Authentication.Key,
			Port:     int32(def.Authentication.Port),
		}
	}

	// Convert Resources
	protoDef.Resources = convertResources(&def.Resources)

	return protoDef
}

func convertResources(res *types.Resources) *aquariumv2.Resources {
	if res == nil {
		return nil
	}

	protoRes := &aquariumv2.Resources{
		Cpu:          uint32(res.Cpu),
		CpuOverbook:  res.CpuOverbook,
		Ram:          uint32(res.Ram),
		RamOverbook:  res.RamOverbook,
		Multitenancy: res.Multitenancy,
		Network:      res.Network,
		Lifetime:     res.Lifetime,
		NodeFilter:   res.NodeFilter,
	}

	if res.Slots != nil {
		slots := uint32(*res.Slots)
		protoRes.Slots = &slots
	}

	if res.Disks != nil {
		protoRes.Disks = make(map[string]*aquariumv2.ResourcesDisk)
		for k, v := range res.Disks {
			protoRes.Disks[k] = &aquariumv2.ResourcesDisk{
				Size:  uint32(v.Size),
				Type:  v.Type,
				Label: v.Label,
				Clone: v.Clone,
				Reuse: v.Reuse,
			}
		}
	}

	return protoRes
}

func convertLabelDefinitionFromProto(def *aquariumv2.LabelDefinition) (types.LabelDefinition, error) {
	options, err := StructToUnparsedJSON(def.GetOptions())
	if err != nil {
		return types.LabelDefinition{}, fmt.Errorf("invalid options: %w", err)
	}

	labelDef := types.LabelDefinition{
		Driver:  def.GetDriver(),
		Options: options,
	}

	// Convert resources if present
	if def.GetResources() != nil {
		labelDef.Resources = convertResourcesFromProto(def.GetResources())
	}

	// Convert authentication if present
	if def.GetAuthentication() != nil {
		labelDef.Authentication = convertAuthenticationFromProto(def.GetAuthentication())
	}

	return labelDef, nil
}

func convertResourcesFromProto(res *aquariumv2.Resources) types.Resources {
	resources := types.Resources{
		Cpu:          uint(res.GetCpu()),
		Ram:          uint(res.GetRam()),
		CpuOverbook:  res.GetCpuOverbook(),
		RamOverbook:  res.GetRamOverbook(),
		Multitenancy: res.GetMultitenancy(),
		Network:      res.GetNetwork(),
		NodeFilter:   res.GetNodeFilter(),
		Lifetime:     res.GetLifetime(),
	}

	if res.Slots != nil {
		slots := uint(res.GetSlots())
		resources.Slots = &slots
	}

	if res.GetDisks() != nil {
		resources.Disks = make(map[string]types.ResourcesDisk)
		for k, v := range res.GetDisks() {
			if v != nil {
				resources.Disks[k] = convertResourcesDiskFromProto(v)
			}
		}
	}

	return resources
}

func convertResourcesDiskFromProto(disk *aquariumv2.ResourcesDisk) types.ResourcesDisk {
	return types.ResourcesDisk{
		Type:  disk.GetType(),
		Label: disk.GetLabel(),
		Size:  uint(disk.GetSize()),
		Clone: disk.GetClone(),
		Reuse: disk.GetReuse(),
	}
}

func convertAuthenticationFromProto(auth *aquariumv2.Authentication) *types.Authentication {
	return &types.Authentication{
		Username: auth.GetUsername(),
		Password: auth.GetPassword(),
		Key:      auth.GetKey(),
		Port:     int(auth.GetPort()),
	}
}

func convertNodeDefinition(def *types.NodeDefinition) *aquariumv2.NodeDefinition {
	if def == nil {
		return nil
	}

	protoDef := &aquariumv2.NodeDefinition{}

	// Convert CPU info
	protoDef.Cpu = make([]*aquariumv2.CpuInfo, len(def.Cpu))
	for i, cpu := range def.Cpu {
		protoDef.Cpu[i] = &aquariumv2.CpuInfo{
			Cores:      cpu.Cores,
			ModelName:  cpu.ModelName,
			Mhz:        float32(cpu.Mhz),
			CacheSize:  strconv.FormatInt(int64(cpu.CacheSize), 10),
			Microcode:  cpu.Microcode,
			VendorId:   cpu.VendorID,
			PhysicalId: cpu.PhysicalID,
			CoreId:     cpu.CoreID,
			Family:     cpu.Family,
			Model:      cpu.Model,
			Stepping:   strconv.FormatInt(int64(cpu.Stepping), 10),
		}
	}

	// Convert Memory info
	if def.Memory != nil {
		protoDef.Memory = &aquariumv2.MemoryInfo{
			Total:       def.Memory.Total,
			Available:   def.Memory.Available,
			Used:        def.Memory.Used,
			UsedPercent: float32(def.Memory.UsedPercent),
		}
	}

	// Convert Host info
	if def.Host != nil {
		protoDef.Host = &aquariumv2.HostInfo{
			Hostname:        def.Host.Hostname,
			Os:              def.Host.OS,
			Platform:        def.Host.Platform,
			PlatformFamily:  def.Host.PlatformFamily,
			PlatformVersion: def.Host.PlatformVersion,
			KernelVersion:   def.Host.KernelVersion,
			KernelArch:      def.Host.KernelArch,
		}
	}

	// Convert Network interfaces
	protoDef.Nets = make([]*aquariumv2.NetworkInterface, len(def.Nets))
	for i, net := range def.Nets {
		protoDef.Nets[i] = &aquariumv2.NetworkInterface{
			Name:  net.Name,
			Addrs: make([]string, len(net.Addrs)),
			Flags: net.Flags,
		}
		for j, addr := range net.Addrs {
			protoDef.Nets[i].Addrs[j] = addr.Addr
		}
	}

	// Convert Disk usage
	if def.Disks != nil {
		protoDef.Disks = make(map[string]*aquariumv2.DiskUsage)
		for k, v := range def.Disks {
			if v != nil {
				protoDef.Disks[k] = &aquariumv2.DiskUsage{
					Path:        v.Path,
					Fstype:      v.Fstype,
					Total:       v.Total,
					Free:        v.Free,
					Used:        v.Used,
					UsedPercent: float32(v.UsedPercent),
				}
			}
		}
	}

	return protoDef
}
