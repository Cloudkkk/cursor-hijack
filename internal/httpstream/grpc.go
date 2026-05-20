package httpstream

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// GRPCFrame represents a single gRPC message frame.
// gRPC uses length-prefixed framing: [1-byte compressed flag][4-byte length][message]
type GRPCFrame struct {
	Compressed bool   // Frame compressed flag (header[0] == 1)
	Data       []byte // Message data (decompressed if compressed flag was set)
	RawData    []byte // Original raw data (for debugging if decompression fails)
}

// GRPCMessage represents a parsed gRPC message.
type GRPCMessage struct {
	Service    string      // e.g., "aiserver.v1.RepositoryService"
	Method     string      // e.g., "SyncMerkleSubtreeV2"
	FullMethod string      // e.g., "/aiserver.v1.RepositoryService/SyncMerkleSubtreeV2"
	Direction  Direction   // C2S (request) or S2C (response)
	Frame      *GRPCFrame  // Raw frame
	Message    interface{} // Deserialized protobuf message (if available)
	JSON       string      // JSON representation (if deserialized)
	Error      string      // Parsing error (if any)

	// Streaming info
	IsStreaming bool // Is this from a streaming RPC
	FrameIndex  int  // Frame index in streaming (0-based)
	Compressed  bool // Frame compressed flag
}

// GRPCParser parses gRPC frames and messages.
type GRPCParser struct {
	registry *MessageRegistry
}

// NewGRPCParser creates a new gRPC parser.
func NewGRPCParser(registry *MessageRegistry) *GRPCParser {
	return &GRPCParser{registry: registry}
}

// decompressGzip decompresses gzip data.
func decompressGzip(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader error: %w", err)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// ParseMethodFromURL extracts service and method from gRPC URL path.
// Format: /package.Service/Method
func ParseMethodFromURL(url string) (service, method, fullMethod string) {
	fullMethod = url
	// Remove leading slash
	path := strings.TrimPrefix(url, "/")

	// Split by last /
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return "", "", fullMethod
	}

	service = path[:idx]
	method = path[idx+1:]
	return service, method, fullMethod
}

// ReadFrame reads a single gRPC frame from the reader.
// Returns nil, io.EOF when no more frames.
// gRPC framing: [1-byte compressed flag][4-byte length][message]
// When compressed flag = 1, message is gzip compressed (gRPC standard).
func (p *GRPCParser) ReadFrame(r io.Reader) (*GRPCFrame, error) {
	// Read 5-byte header: [compressed:1][length:4]
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	compressed := header[0] == 1
	length := binary.BigEndian.Uint32(header[1:5])

	// Sanity check - max 16MB
	if length > 16*1024*1024 {
		return nil, fmt.Errorf("gRPC frame too large: %d bytes", length)
	}

	// Read message data
	rawData := make([]byte, length)
	if _, err := io.ReadFull(r, rawData); err != nil {
		return nil, err
	}

	frame := &GRPCFrame{
		Compressed: compressed,
		RawData:    rawData,
	}

	// Decompress if compressed flag is set (gRPC uses gzip)
	if compressed {
		decompressed, err := decompressGzip(rawData)
		if err != nil {
			// Keep raw data for debugging, Data will be nil
			frame.Data = nil
		} else {
			frame.Data = decompressed
		}
	} else {
		frame.Data = rawData
	}

	return frame, nil
}

// ReadAllFrames reads all gRPC frames from the reader.
func (p *GRPCParser) ReadAllFrames(r io.Reader) ([]*GRPCFrame, error) {
	var frames []*GRPCFrame
	for {
		frame, err := p.ReadFrame(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return frames, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

// ParseMessage parses a gRPC message using the registry.
func (p *GRPCParser) ParseMessage(frame *GRPCFrame, service, method string, isRequest bool) *GRPCMessage {
	msg := &GRPCMessage{
		Service:    service,
		Method:     method,
		FullMethod: "/" + service + "/" + method,
		Frame:      frame,
		Compressed: frame.Compressed,
	}

	if isRequest {
		msg.Direction = ClientToServer
	} else {
		msg.Direction = ServerToClient
	}

	// frame.Data is already decompressed (or nil if decompression failed)
	data := frame.Data
	
	// Decompression failed
	if frame.Compressed && data == nil {
		msg.Error = "gzip decompression failed"
		return msg
	}

	// Handle empty message "{}" (2 bytes) - no protobuf deserialization needed
	if len(data) == 2 && string(data) == "{}" {
		msg.JSON = "{}"
		return msg
	}

	// Handle empty protobuf (0 bytes) - valid empty message
	if len(data) == 0 {
		msg.JSON = "{}"
		return msg
	}

	if p.registry == nil {
		msg.Error = "no message registry"
		return msg
	}

	// Look up message type
	var msgType protoreflect.MessageType
	if isRequest {
		msgType = p.registry.GetRequestType(service, method)
	} else {
		msgType = p.registry.GetResponseType(service, method)
	}

	if msgType == nil {
		msg.Error = fmt.Sprintf("unknown message type for %s/%s (request=%v)", service, method, isRequest)
		return msg
	}

	// Create new message instance and unmarshal
	protoMsg := msgType.New().Interface()
	if err := proto.Unmarshal(data, protoMsg); err != nil {
		msg.Error = fmt.Sprintf("unmarshal error: %v", err)
		return msg
	}

	msg.Message = protoMsg

	// Convert to JSON for logging
	jsonBytes, err := protojson.MarshalOptions{
		Multiline:       false,
		EmitUnpopulated: false,
	}.Marshal(protoMsg)
	if err != nil {
		msg.Error = fmt.Sprintf("json marshal error: %v", err)
	} else {
		msg.JSON = string(jsonBytes)
	}

	return msg
}

// ContentTypeInfo describes the content type for gRPC/Connect parsing.
type ContentTypeInfo struct {
	IsGRPC               bool // Standard gRPC with length-prefixed framing
	IsConnectProto       bool // Connect Protocol unary with raw protobuf (no framing)
	IsConnectStreamProto bool // Connect Protocol streaming with envelope framing
	IsConnectJSON        bool // Connect Protocol with JSON
}

// ParseContentType analyzes content type for gRPC/Connect protocols.
func ParseContentType(contentType string) ContentTypeInfo {
	ct := strings.ToLower(contentType)
	return ContentTypeInfo{
		IsGRPC:               strings.HasPrefix(ct, "application/grpc"),
		IsConnectProto:       ct == "application/proto" || strings.HasPrefix(ct, "application/proto;"),
		IsConnectStreamProto: strings.HasPrefix(ct, "application/connect+proto"),
		IsConnectJSON:        ct == "application/json" || strings.HasPrefix(ct, "application/json;"),
	}
}

// IsGRPCContentType checks if the content type is gRPC or Connect Protocol.
func IsGRPCContentType(contentType string) bool {
	info := ParseContentType(contentType)
	return info.IsGRPC || info.IsConnectProto || info.IsConnectStreamProto
}

// HasEnvelopeFraming checks if the content type uses envelope/length-prefixed framing.
func (c ContentTypeInfo) HasEnvelopeFraming() bool {
	return c.IsGRPC || c.IsConnectStreamProto
}

// MessageRegistry maps service/method to protobuf message types.
type MessageRegistry struct {
	mu        sync.RWMutex
	requests  map[string]protoreflect.MessageType // "service/method" -> request type
	responses map[string]protoreflect.MessageType // "service/method" -> response type
}

// NewMessageRegistry creates a new message registry.
func NewMessageRegistry() *MessageRegistry {
	return &MessageRegistry{
		requests:  make(map[string]protoreflect.MessageType),
		responses: make(map[string]protoreflect.MessageType),
	}
}

// Register registers request and response types for a method.
func (r *MessageRegistry) Register(service, method string, reqType, respType protoreflect.MessageType) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := service + "/" + method
	if reqType != nil {
		r.requests[key] = reqType
	}
	if respType != nil {
		r.responses[key] = respType
	}
}

// RegisterByName registers types by their full name.
func (r *MessageRegistry) RegisterByName(service, method, reqTypeName, respTypeName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := service + "/" + method

	if reqTypeName != "" {
		mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(reqTypeName))
		if err != nil {
			return fmt.Errorf("request type %s not found: %w", reqTypeName, err)
		}
		r.requests[key] = mt
	}

	if respTypeName != "" {
		mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(respTypeName))
		if err != nil {
			return fmt.Errorf("response type %s not found: %w", respTypeName, err)
		}
		r.responses[key] = mt
	}

	return nil
}

// GetRequestType returns the request message type for a method.
func (r *MessageRegistry) GetRequestType(service, method string) protoreflect.MessageType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.requests[service+"/"+method]
}

// GetResponseType returns the response message type for a method.
func (r *MessageRegistry) GetResponseType(service, method string) protoreflect.MessageType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.responses[service+"/"+method]
}

// TryParseFromGlobalRegistry attempts to find message types using multiple strategies:
// 1. Service descriptor lookup (most accurate)
// 2. Naming convention patterns
func (r *MessageRegistry) TryParseFromGlobalRegistry(service, method string) bool {
	// Strategy 1: Try to find via service descriptor (most accurate)
	if r.tryFromServiceDescriptor(service, method) {
		return true
	}

	// Strategy 2: Try naming convention patterns
	return r.tryFromNamingConventions(service, method)
}

// tryFromServiceDescriptor looks up the service descriptor to get exact input/output types.
func (r *MessageRegistry) tryFromServiceDescriptor(service, method string) bool {
	// Find the service type
	sd, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return false
	}

	serviceDesc, ok := sd.(protoreflect.ServiceDescriptor)
	if !ok {
		return false
	}

	// Find the method
	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(method))
	if methodDesc == nil {
		return false
	}

	// Get input and output types
	inputType := methodDesc.Input()
	outputType := methodDesc.Output()

	r.mu.Lock()
	defer r.mu.Unlock()
	key := service + "/" + method

	// Find and register the message types
	if inputType != nil {
		if mt, err := protoregistry.GlobalTypes.FindMessageByName(inputType.FullName()); err == nil {
			r.requests[key] = mt
		}
	}
	if outputType != nil {
		if mt, err := protoregistry.GlobalTypes.FindMessageByName(outputType.FullName()); err == nil {
			r.responses[key] = mt
		}
	}

	return true
}

// tryFromNamingConventions tries common naming patterns.
func (r *MessageRegistry) tryFromNamingConventions(service, method string) bool {
	// Extract package from service name
	// e.g., "aiserver.v1.RepositoryService" -> "aiserver.v1"
	lastDot := strings.LastIndex(service, ".")
	if lastDot == -1 {
		return false
	}
	pkg := service[:lastDot]

	// Try common naming patterns
	patterns := []struct {
		reqSuffix  string
		respSuffix string
	}{
		{"Request", "Response"},
		{"Req", "Resp"},
		{"", "Response"},
	}

	for _, p := range patterns {
		reqName := pkg + "." + method + p.reqSuffix
		respName := pkg + "." + method + p.respSuffix

		reqType, reqErr := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(reqName))
		respType, respErr := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(respName))

		if reqErr == nil || respErr == nil {
			r.mu.Lock()
			key := service + "/" + method
			if reqErr == nil {
				r.requests[key] = reqType
			}
			if respErr == nil {
				r.responses[key] = respType
			}
			r.mu.Unlock()
			return true
		}
	}

	return false
}

// ParseGRPCBody parses gRPC body and returns messages.
// Handles:
// - Standard gRPC (application/grpc*): length-prefixed framing
// - Connect Protocol unary (application/proto): raw protobuf, no framing
// - Connect Protocol streaming (application/connect+proto): envelope framing
func ParseGRPCBody(body []byte, service, method string, isRequest bool, registry *MessageRegistry, contentType string) []*GRPCMessage {
	ctInfo := ParseContentType(contentType)

	// Connect Protocol streaming or standard gRPC: envelope/length-prefixed framing
	if ctInfo.HasEnvelopeFraming() {
		return parseGRPCFramedBody(body, service, method, isRequest, registry)
	}

	// Connect Protocol unary: raw protobuf without framing
	if ctInfo.IsConnectProto {
		return parseConnectProtoBody(body, service, method, isRequest, registry)
	}

	// Fallback: try as raw protobuf
	return parseConnectProtoBody(body, service, method, isRequest, registry)
}

// parseConnectProtoBody parses Connect Protocol body (raw protobuf).
func parseConnectProtoBody(body []byte, service, method string, isRequest bool, registry *MessageRegistry) []*GRPCMessage {
	parser := NewGRPCParser(registry)

	// Create a single frame with the entire body (no length prefix)
	frame := &GRPCFrame{
		Compressed: false,
		Data:       body,
	}

	msg := parser.ParseMessage(frame, service, method, isRequest)
	return []*GRPCMessage{msg}
}

// parseGRPCFramedBody parses standard gRPC body with length-prefixed framing.
func parseGRPCFramedBody(body []byte, service, method string, isRequest bool, registry *MessageRegistry) []*GRPCMessage {
	parser := NewGRPCParser(registry)
	reader := bytes.NewReader(body)

	var messages []*GRPCMessage
	for {
		frame, err := parser.ReadFrame(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			messages = append(messages, &GRPCMessage{
				Service:    service,
				Method:     method,
				FullMethod: "/" + service + "/" + method,
				Error:      fmt.Sprintf("frame read error: %v", err),
			})
			break
		}

		msg := parser.ParseMessage(frame, service, method, isRequest)
		messages = append(messages, msg)
	}

	return messages
}
