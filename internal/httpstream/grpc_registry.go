package httpstream

import (
	// Import generated protobuf packages to register them with global registry
	_ "github.com/burpheart/cursor-tap/cursor_proto/gen/agent/v1"
	_ "github.com/burpheart/cursor-tap/cursor_proto/gen/aiserver/v1"
	_ "github.com/burpheart/cursor-tap/cursor_proto/gen/anyrun/v1"
	_ "github.com/burpheart/cursor-tap/cursor_proto/gen/internapi/v1"
)

// DefaultGRPCRegistry returns a new registry that auto-discovers types
// from the global protobuf registry.
func DefaultGRPCRegistry() *MessageRegistry {
	return NewMessageRegistry()
}

// RegisterKnownServices registers common service method mappings.
// Call this after importing the generated protobuf packages.
func RegisterKnownServices(r *MessageRegistry) {
	// The registry uses TryParseFromGlobalRegistry to auto-discover
	// message types based on naming conventions.
	// Manual registration is only needed for non-standard naming.
	
	// Example manual registration (if needed):
	// r.RegisterByName("aiserver.v1.RepositoryService", "SyncMerkleSubtreeV2",
	//     "aiserver.v1.SyncMerkleSubtreeV2Request",
	//     "aiserver.v1.SyncMerkleSubtreeV2Response")
}
