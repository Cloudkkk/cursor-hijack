package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "用法: %s <proto文件> <消息名>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "示例: %s agent_v1.proto AgentClientMessage\n", os.Args[0])
		os.Exit(1)
	}

	protoFile := os.Args[1]
	messageName := os.Args[2]

	// 获取 proto 文件所在目录作为搜索路径
	protoDir := filepath.Dir(protoFile)
	if protoDir == "" || protoDir == "." {
		protoDir, _ = os.Getwd()
	}

	// 创建编译器，配置导入路径，使用自定义 resolver 支持 google protobuf
	compiler := protocompile.Compiler{
		Resolver: &combinedResolver{
			sourceResolver: &protocompile.SourceResolver{
				ImportPaths: []string{
					protoDir,
					".", // 当前目录
				},
			},
		},
	}

	// 编译 proto 文件
	files, err := compiler.Compile(context.Background(), filepath.Base(protoFile))
	if err != nil {
		fmt.Fprintf(os.Stderr, "编译错误: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "没有找到编译结果\n")
		os.Exit(1)
	}

	// 获取主文件
	mainFile := files[0]

	// 查找目标消息
	var targetMsg protoreflect.MessageDescriptor
	msgs := mainFile.Messages()
	for i := 0; i < msgs.Len(); i++ {
		msg := msgs.Get(i)
		if string(msg.Name()) == messageName {
			targetMsg = msg
			break
		}
	}

	if targetMsg == nil {
		fmt.Fprintf(os.Stderr, "未找到消息: %s\n", messageName)
		fmt.Fprintf(os.Stderr, "可用的消息:\n")
		for i := 0; i < msgs.Len(); i++ {
			fmt.Fprintf(os.Stderr, "  - %s\n", msgs.Get(i).Name())
		}
		os.Exit(1)
	}

	// 收集所有依赖的类型
	inliner := &ProtoInliner{
		files:          files,
		collectedMsgs:  make(map[string]protoreflect.MessageDescriptor),
		collectedEnums: make(map[string]protoreflect.EnumDescriptor),
	}

	inliner.collectDependencies(targetMsg)

	// 生成内联的 proto
	output := inliner.generate(targetMsg, string(mainFile.Package()))
	fmt.Println(output)
}

type ProtoInliner struct {
	files          linker.Files
	collectedMsgs  map[string]protoreflect.MessageDescriptor
	collectedEnums map[string]protoreflect.EnumDescriptor
}

func (p *ProtoInliner) collectDependencies(msg protoreflect.MessageDescriptor) {
	fullName := string(msg.FullName())
	if _, exists := p.collectedMsgs[fullName]; exists {
		return
	}
	p.collectedMsgs[fullName] = msg

	// 收集嵌套消息
	nested := msg.Messages()
	for i := 0; i < nested.Len(); i++ {
		p.collectDependencies(nested.Get(i))
	}

	// 收集嵌套枚举
	enums := msg.Enums()
	for i := 0; i < enums.Len(); i++ {
		enum := enums.Get(i)
		p.collectedEnums[string(enum.FullName())] = enum
	}

	// 收集字段引用的类型
	fields := msg.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		p.collectFieldDependencies(field)
	}

	// 收集 oneof 中的字段
	oneofs := msg.Oneofs()
	for i := 0; i < oneofs.Len(); i++ {
		oneof := oneofs.Get(i)
		for j := 0; j < oneof.Fields().Len(); j++ {
			p.collectFieldDependencies(oneof.Fields().Get(j))
		}
	}
}

func (p *ProtoInliner) collectFieldDependencies(field protoreflect.FieldDescriptor) {
	switch field.Kind() {
	case protoreflect.MessageKind:
		refMsg := field.Message()
		if refMsg != nil && !p.isGoogleType(refMsg.FullName()) {
			p.collectDependencies(refMsg)
		}
	case protoreflect.EnumKind:
		refEnum := field.Enum()
		if refEnum != nil && !p.isGoogleType(refEnum.FullName()) {
			p.collectedEnums[string(refEnum.FullName())] = refEnum
		}
	}

	// 处理 map 类型的值
	if field.IsMap() {
		valueField := field.MapValue()
		if valueField != nil {
			p.collectFieldDependencies(valueField)
		}
	}
}

func (p *ProtoInliner) isGoogleType(name protoreflect.FullName) bool {
	s := string(name)
	return strings.HasPrefix(s, "google.protobuf.") || strings.HasPrefix(s, "google.rpc.")
}

func (p *ProtoInliner) generate(rootMsg protoreflect.MessageDescriptor, pkg string) string {
	var sb strings.Builder

	sb.WriteString("// 自动生成的内联 proto 文件\n")
	sb.WriteString("// 原始消息: " + string(rootMsg.FullName()) + "\n\n")
	sb.WriteString(`syntax = "proto3";` + "\n\n")

	// 收集需要生成的顶层类型（排除嵌套类型）
	topLevelMsgs := make([]protoreflect.MessageDescriptor, 0)
	topLevelEnums := make([]protoreflect.EnumDescriptor, 0)

	for _, msg := range p.collectedMsgs {
		// 只输出顶层消息（Parent 是 FileDescriptor 或者是根消息）
		if msg.Parent() == msg.ParentFile() {
			topLevelMsgs = append(topLevelMsgs, msg)
		}
	}

	for _, enum := range p.collectedEnums {
		// 只输出顶层枚举
		if enum.Parent() == enum.ParentFile() {
			topLevelEnums = append(topLevelEnums, enum)
		}
	}

	// 按名称排序
	sort.Slice(topLevelMsgs, func(i, j int) bool {
		return topLevelMsgs[i].FullName() < topLevelMsgs[j].FullName()
	})
	sort.Slice(topLevelEnums, func(i, j int) bool {
		return topLevelEnums[i].FullName() < topLevelEnums[j].FullName()
	})

	// 先输出枚举
	for _, enum := range topLevelEnums {
		p.writeEnum(&sb, enum, 0)
		sb.WriteString("\n")
	}

	// 输出消息
	for _, msg := range topLevelMsgs {
		p.writeMessage(&sb, msg, 0)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (p *ProtoInliner) writeEnum(sb *strings.Builder, enum protoreflect.EnumDescriptor, indent int) {
	indentStr := strings.Repeat("  ", indent)

	sb.WriteString(fmt.Sprintf("%s// From: %s\n", indentStr, enum.FullName()))
	sb.WriteString(fmt.Sprintf("%senum %s {\n", indentStr, enum.Name()))

	values := enum.Values()
	for i := 0; i < values.Len(); i++ {
		v := values.Get(i)
		sb.WriteString(fmt.Sprintf("%s  %s = %d;\n", indentStr, v.Name(), v.Number()))
	}

	sb.WriteString(fmt.Sprintf("%s}\n", indentStr))
}

func (p *ProtoInliner) writeMessage(sb *strings.Builder, msg protoreflect.MessageDescriptor, indent int) {
	indentStr := strings.Repeat("  ", indent)

	sb.WriteString(fmt.Sprintf("%s// From: %s\n", indentStr, msg.FullName()))
	sb.WriteString(fmt.Sprintf("%smessage %s {\n", indentStr, msg.Name()))

	// 写入嵌套枚举
	enums := msg.Enums()
	for i := 0; i < enums.Len(); i++ {
		p.writeEnum(sb, enums.Get(i), indent+1)
	}

	// 写入嵌套消息
	nested := msg.Messages()
	for i := 0; i < nested.Len(); i++ {
		// 跳过 map entry 类型
		if !nested.Get(i).IsMapEntry() {
			p.writeMessage(sb, nested.Get(i), indent+1)
		}
	}

	// 收集 oneof 信息 (跳过 synthetic oneof - proto3 optional 生成的以 _ 开头的 oneof)
	oneofFields := make(map[string][]protoreflect.FieldDescriptor)
	oneofs := msg.Oneofs()
	for i := 0; i < oneofs.Len(); i++ {
		oneof := oneofs.Get(i)
		name := string(oneof.Name())
		// 跳过 synthetic oneof
		if strings.HasPrefix(name, "_") {
			continue
		}
		fields := oneof.Fields()
		for j := 0; j < fields.Len(); j++ {
			oneofFields[name] = append(oneofFields[name], fields.Get(j))
		}
	}

	// 写入普通字段
	fields := msg.Fields()
	writtenOneofs := make(map[string]bool)

	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)

		// 检查是否属于 oneof
		if oneof := field.ContainingOneof(); oneof != nil {
			oneofName := string(oneof.Name())
			
			// 跳过 synthetic oneof (proto3 optional 字段会生成以 _ 开头的 oneof)
			// 这些字段直接作为 optional 输出
			if strings.HasPrefix(oneofName, "_") {
				p.writeField(sb, field, indent+1)
				continue
			}
			
			if !writtenOneofs[oneofName] {
				writtenOneofs[oneofName] = true
				sb.WriteString(fmt.Sprintf("%s  oneof %s {\n", indentStr, oneofName))
				for _, f := range oneofFields[oneofName] {
					p.writeField(sb, f, indent+2)
				}
				sb.WriteString(fmt.Sprintf("%s  }\n", indentStr))
			}
		} else {
			p.writeField(sb, field, indent+1)
		}
	}

	sb.WriteString(fmt.Sprintf("%s}\n", indentStr))
}

func (p *ProtoInliner) writeField(sb *strings.Builder, field protoreflect.FieldDescriptor, indent int) {
	indentStr := strings.Repeat("  ", indent)

	var typeStr string

	if field.IsMap() {
		keyType := p.kindToString(field.MapKey().Kind(), field.MapKey())
		valueType := p.kindToString(field.MapValue().Kind(), field.MapValue())
		typeStr = fmt.Sprintf("map<%s, %s>", keyType, valueType)
	} else {
		typeStr = p.kindToString(field.Kind(), field)
		if field.IsList() {
			typeStr = "repeated " + typeStr
		} else if field.HasOptionalKeyword() {
			typeStr = "optional " + typeStr
		}
	}

	sb.WriteString(fmt.Sprintf("%s%s %s = %d;\n", indentStr, typeStr, field.Name(), field.Number()))
}

func (p *ProtoInliner) kindToString(kind protoreflect.Kind, field protoreflect.FieldDescriptor) string {
	switch kind {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind:
		return "int32"
	case protoreflect.Sint32Kind:
		return "sint32"
	case protoreflect.Uint32Kind:
		return "uint32"
	case protoreflect.Int64Kind:
		return "int64"
	case protoreflect.Sint64Kind:
		return "sint64"
	case protoreflect.Uint64Kind:
		return "uint64"
	case protoreflect.Sfixed32Kind:
		return "sfixed32"
	case protoreflect.Fixed32Kind:
		return "fixed32"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.Sfixed64Kind:
		return "sfixed64"
	case protoreflect.Fixed64Kind:
		return "fixed64"
	case protoreflect.DoubleKind:
		return "double"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "bytes"
	case protoreflect.MessageKind:
		if field != nil && field.Message() != nil {
			msg := field.Message()
			if p.isGoogleType(msg.FullName()) {
				// Google 类型转换为 bytes 或 string
				return p.googleTypeReplacement(msg.FullName())
			}
			return string(msg.Name())
		}
		return "bytes"
	case protoreflect.EnumKind:
		if field != nil && field.Enum() != nil {
			enum := field.Enum()
			if p.isGoogleType(enum.FullName()) {
				return "int32"
			}
			return string(enum.Name())
		}
		return "int32"
	default:
		return "bytes"
	}
}

func (p *ProtoInliner) googleTypeReplacement(name protoreflect.FullName) string {
	s := string(name)
	switch s {
	case "google.protobuf.Timestamp":
		return "int64" // Unix timestamp
	case "google.protobuf.Duration":
		return "int64" // Duration in nanoseconds
	case "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.ListValue":
		return "bytes" // JSON-like structure
	case "google.protobuf.Any":
		return "bytes"
	case "google.protobuf.Empty":
		return "bytes"
	case "google.protobuf.BoolValue":
		return "bool"
	case "google.protobuf.StringValue":
		return "string"
	case "google.protobuf.BytesValue":
		return "bytes"
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return "int32"
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return "int64"
	case "google.protobuf.FloatValue":
		return "float"
	case "google.protobuf.DoubleValue":
		return "double"
	case "google.rpc.Status":
		return "bytes"
	default:
		return "bytes"
	}
}

// combinedResolver 组合 source resolver 和 well-known types
type combinedResolver struct {
	sourceResolver *protocompile.SourceResolver
}

func (r *combinedResolver) FindFileByPath(path string) (protocompile.SearchResult, error) {
	// 首先检查是否是 google 标准类型
	if strings.HasPrefix(path, "google/") {
		content, ok := wellKnownTypes[path]
		if ok {
			return protocompile.SearchResult{
				Source: strings.NewReader(content),
			}, nil
		}
	}

	// 否则使用 source resolver
	return r.sourceResolver.FindFileByPath(path)
}

// Google well-known types 的最小定义
var wellKnownTypes = map[string]string{
	"google/protobuf/struct.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/structpb";

message Struct { map<string, Value> fields = 1; }
message Value {
  oneof kind {
    NullValue null_value = 1;
    double number_value = 2;
    string string_value = 3;
    bool bool_value = 4;
    Struct struct_value = 5;
    ListValue list_value = 6;
  }
}
message ListValue { repeated Value values = 1; }
enum NullValue { NULL_VALUE = 0; }
`,
	"google/protobuf/timestamp.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/timestamppb";
message Timestamp { int64 seconds = 1; int32 nanos = 2; }
`,
	"google/protobuf/duration.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/durationpb";
message Duration { int64 seconds = 1; int32 nanos = 2; }
`,
	"google/protobuf/any.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/anypb";
message Any { string type_url = 1; bytes value = 2; }
`,
	"google/protobuf/empty.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/emptypb";
message Empty {}
`,
	"google/protobuf/wrappers.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/wrapperspb";
message DoubleValue { double value = 1; }
message FloatValue { float value = 1; }
message Int64Value { int64 value = 1; }
message UInt64Value { uint64 value = 1; }
message Int32Value { int32 value = 1; }
message UInt32Value { uint32 value = 1; }
message BoolValue { bool value = 1; }
message StringValue { string value = 1; }
message BytesValue { bytes value = 1; }
`,
	"google/protobuf/field_mask.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/known/fieldmaskpb";
message FieldMask { repeated string paths = 1; }
`,
	"google/protobuf/descriptor.proto": `
syntax = "proto3";
package google.protobuf;
option go_package = "google.golang.org/protobuf/types/descriptorpb";
message FileDescriptorSet { repeated FileDescriptorProto file = 1; }
message FileDescriptorProto { optional string name = 1; }
message DescriptorProto { optional string name = 1; }
message FieldDescriptorProto { optional string name = 1; }
message EnumDescriptorProto { optional string name = 1; }
`,
	"google/rpc/status.proto": `
syntax = "proto3";
package google.rpc;
option go_package = "google.golang.org/genproto/googleapis/rpc/status;status";
import "google/protobuf/any.proto";
message Status { int32 code = 1; string message = 2; repeated google.protobuf.Any details = 3; }
`,
	"google/rpc/code.proto": `
syntax = "proto3";
package google.rpc;
option go_package = "google.golang.org/genproto/googleapis/rpc/code;code";
enum Code {
  OK = 0; CANCELLED = 1; UNKNOWN = 2; INVALID_ARGUMENT = 3; DEADLINE_EXCEEDED = 4;
  NOT_FOUND = 5; ALREADY_EXISTS = 6; PERMISSION_DENIED = 7; RESOURCE_EXHAUSTED = 8;
  FAILED_PRECONDITION = 9; ABORTED = 10; OUT_OF_RANGE = 11; UNIMPLEMENTED = 12;
  INTERNAL = 13; UNAVAILABLE = 14; DATA_LOSS = 15; UNAUTHENTICATED = 16;
}
`,
}
