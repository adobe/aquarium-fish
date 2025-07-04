// Copyright 2025 Adobe. All rights reserved.
// This file is licensed to you under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License. You may obtain a copy
// of the License at http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed under
// the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
// OF ANY KIND, either express or implied. See the License for the specific language
// governing permissions and limitations under the License.

// Author: Sergei Parshev (@sparshev)

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: aquarium/v2/vote.proto

package aquariumv2

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// Vote represents the current state of Application election process of specific node
//
// When Application becomes available for the node it starts to vote to notify the cluster
// about its availability. Votes are basically "yes" or "no" and could take a number of rounds
// depends on the cluster voting and election rules.
// Votes are not stored in DB and lives only in-memory.
type Vote struct {
	state          protoimpl.MessageState `protogen:"open.v1"`
	Uid            string                 `protobuf:"bytes,1,opt,name=uid,proto3" json:"uid,omitempty"`
	CreatedAt      *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	ApplicationUid string                 `protobuf:"bytes,3,opt,name=application_uid,json=applicationUid,proto3" json:"application_uid,omitempty"`
	NodeUid        string                 `protobuf:"bytes,4,opt,name=node_uid,json=nodeUid,proto3" json:"node_uid,omitempty"`
	// Round of the election, because it can take a number of rounds to figure out the Only One.
	Round uint32 `protobuf:"varint,5,opt,name=round,proto3" json:"round,omitempty"`
	// Node places answer to the Vote for the Application's definitions, the number represents
	// the first available index of the definition which fits the node available resources. In
	// case it's `-1` then node can't run any of the definitions.
	Available int32 `protobuf:"varint,6,opt,name=available,proto3" json:"available,omitempty"`
	// The custom rule result is needed to store the custom rule decision
	RuleResult uint32 `protobuf:"varint,7,opt,name=rule_result,json=ruleResult,proto3" json:"rule_result,omitempty"`
	// The last resort to figure out for the winner.
	Rand          uint32 `protobuf:"varint,8,opt,name=rand,proto3" json:"rand,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Vote) Reset() {
	*x = Vote{}
	mi := &file_aquarium_v2_vote_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Vote) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Vote) ProtoMessage() {}

func (x *Vote) ProtoReflect() protoreflect.Message {
	mi := &file_aquarium_v2_vote_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Vote.ProtoReflect.Descriptor instead.
func (*Vote) Descriptor() ([]byte, []int) {
	return file_aquarium_v2_vote_proto_rawDescGZIP(), []int{0}
}

func (x *Vote) GetUid() string {
	if x != nil {
		return x.Uid
	}
	return ""
}

func (x *Vote) GetCreatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.CreatedAt
	}
	return nil
}

func (x *Vote) GetApplicationUid() string {
	if x != nil {
		return x.ApplicationUid
	}
	return ""
}

func (x *Vote) GetNodeUid() string {
	if x != nil {
		return x.NodeUid
	}
	return ""
}

func (x *Vote) GetRound() uint32 {
	if x != nil {
		return x.Round
	}
	return 0
}

func (x *Vote) GetAvailable() int32 {
	if x != nil {
		return x.Available
	}
	return 0
}

func (x *Vote) GetRuleResult() uint32 {
	if x != nil {
		return x.RuleResult
	}
	return 0
}

func (x *Vote) GetRand() uint32 {
	if x != nil {
		return x.Rand
	}
	return 0
}

var File_aquarium_v2_vote_proto protoreflect.FileDescriptor

const file_aquarium_v2_vote_proto_rawDesc = "" +
	"\n" +
	"\x16aquarium/v2/vote.proto\x12\vaquarium.v2\x1a\x1fgoogle/protobuf/timestamp.proto\"\x80\x02\n" +
	"\x04Vote\x12\x10\n" +
	"\x03uid\x18\x01 \x01(\tR\x03uid\x129\n" +
	"\n" +
	"created_at\x18\x02 \x01(\v2\x1a.google.protobuf.TimestampR\tcreatedAt\x12'\n" +
	"\x0fapplication_uid\x18\x03 \x01(\tR\x0eapplicationUid\x12\x19\n" +
	"\bnode_uid\x18\x04 \x01(\tR\anodeUid\x12\x14\n" +
	"\x05round\x18\x05 \x01(\rR\x05round\x12\x1c\n" +
	"\tavailable\x18\x06 \x01(\x05R\tavailable\x12\x1f\n" +
	"\vrule_result\x18\a \x01(\rR\n" +
	"ruleResult\x12\x12\n" +
	"\x04rand\x18\b \x01(\rR\x04randBEZCgithub.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2;aquariumv2b\x06proto3"

var (
	file_aquarium_v2_vote_proto_rawDescOnce sync.Once
	file_aquarium_v2_vote_proto_rawDescData []byte
)

func file_aquarium_v2_vote_proto_rawDescGZIP() []byte {
	file_aquarium_v2_vote_proto_rawDescOnce.Do(func() {
		file_aquarium_v2_vote_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_aquarium_v2_vote_proto_rawDesc), len(file_aquarium_v2_vote_proto_rawDesc)))
	})
	return file_aquarium_v2_vote_proto_rawDescData
}

var file_aquarium_v2_vote_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_aquarium_v2_vote_proto_goTypes = []any{
	(*Vote)(nil),                  // 0: aquarium.v2.Vote
	(*timestamppb.Timestamp)(nil), // 1: google.protobuf.Timestamp
}
var file_aquarium_v2_vote_proto_depIdxs = []int32{
	1, // 0: aquarium.v2.Vote.created_at:type_name -> google.protobuf.Timestamp
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_aquarium_v2_vote_proto_init() }
func file_aquarium_v2_vote_proto_init() {
	if File_aquarium_v2_vote_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_aquarium_v2_vote_proto_rawDesc), len(file_aquarium_v2_vote_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_aquarium_v2_vote_proto_goTypes,
		DependencyIndexes: file_aquarium_v2_vote_proto_depIdxs,
		MessageInfos:      file_aquarium_v2_vote_proto_msgTypes,
	}.Build()
	File_aquarium_v2_vote_proto = out.File
	file_aquarium_v2_vote_proto_goTypes = nil
	file_aquarium_v2_vote_proto_depIdxs = nil
}
