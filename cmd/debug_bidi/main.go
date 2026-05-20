package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	agentv1 "github.com/burpheart/cursor-tap/cursor_proto/gen/agent/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type LogEntry struct {
	Ts         string `json:"ts"`
	Session    string `json:"session"`
	Seq        int    `json:"seq"`
	Direction  string `json:"direction"`
	GrpcMethod string `json:"grpc_method"`
	GrpcData   string `json:"grpc_data"`
}

type BidiAppendData struct {
	Data      string `json:"data"`
	RequestId struct {
		RequestId string `json:"requestId"`
	} `json:"requestId"`
	AppendSeqno string `json:"appendSeqno"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: debug_bidi <jsonl_file> [request_id]")
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	filterRequestId := ""
	if len(os.Args) > 2 {
		filterRequestId = os.Args[2]
	}

	outFile, _ := os.Create("bidi_debug.txt")
	defer outFile.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 500*1024*1024)
	scanner.Buffer(buf, len(buf))

	opts := protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		EmitUnpopulated: false,
	}

	count := 0
	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if entry.GrpcMethod != "BidiAppend" || entry.Direction != "C2S" {
			continue
		}

		var bidiData BidiAppendData
		if err := json.Unmarshal([]byte(entry.GrpcData), &bidiData); err != nil {
			continue
		}

		if filterRequestId != "" && bidiData.RequestId.RequestId != filterRequestId {
			continue
		}

		count++
		fmt.Fprintf(outFile, "\n=== BidiAppend #%d ===\n", count)
		fmt.Fprintf(outFile, "Timestamp: %s\n", entry.Ts)
		fmt.Fprintf(outFile, "Session: %s\n", entry.Session)
		fmt.Fprintf(outFile, "Seq: %d\n", entry.Seq)
		fmt.Fprintf(outFile, "RequestId: %s\n", bidiData.RequestId.RequestId)
		fmt.Fprintf(outFile, "AppendSeqno: %s\n", bidiData.AppendSeqno)
		fmt.Fprintf(outFile, "Data hex length: %d bytes\n", len(bidiData.Data)/2)

		if bidiData.Data == "" {
			fmt.Fprintf(outFile, "Data: (empty)\n")
			continue
		}

		// Decode hex
		rawBytes, err := hex.DecodeString(bidiData.Data)
		if err != nil {
			fmt.Fprintf(outFile, "Hex decode error: %v\n", err)
			continue
		}

		// Parse as AgentClientMessage
		var clientMsg agentv1.AgentClientMessage
		if err := proto.Unmarshal(rawBytes, &clientMsg); err != nil {
			fmt.Fprintf(outFile, "Proto unmarshal error: %v\n", err)
			fmt.Fprintf(outFile, "Raw hex (first 200): %s\n", bidiData.Data[:min(200, len(bidiData.Data))])
			continue
		}

		// Identify message type and output (no truncation)
		switch m := clientMsg.Message.(type) {
		case *agentv1.AgentClientMessage_RunRequest:
			fmt.Fprintf(outFile, "Message Type: runRequest\n")
			if m.RunRequest != nil && m.RunRequest.Action != nil {
				fmt.Fprintf(outFile, "Action present: YES\n")
				actionJSON, _ := opts.Marshal(m.RunRequest.Action)
				fmt.Fprintf(outFile, "Action:\n%s\n", string(actionJSON))
			} else {
				fmt.Fprintf(outFile, "Action present: NO\n")
			}
			// Also output conversationState if present
			if m.RunRequest != nil && m.RunRequest.ConversationState != nil {
				stateJSON, _ := opts.Marshal(m.RunRequest.ConversationState)
				fmt.Fprintf(outFile, "ConversationState:\n%s\n", string(stateJSON))
			}

		case *agentv1.AgentClientMessage_ConversationAction:
			fmt.Fprintf(outFile, "Message Type: conversationAction\n")
			if m.ConversationAction != nil {
				actionJSON, _ := opts.Marshal(m.ConversationAction)
				fmt.Fprintf(outFile, "ConversationAction:\n%s\n", string(actionJSON))
			}

		case *agentv1.AgentClientMessage_ExecClientMessage:
			fmt.Fprintf(outFile, "Message Type: execClientMessage\n")
			jsonData, _ := opts.Marshal(m.ExecClientMessage)
			fmt.Fprintf(outFile, "Content:\n%s\n", string(jsonData))

		case *agentv1.AgentClientMessage_KvClientMessage:
			fmt.Fprintf(outFile, "Message Type: kvClientMessage\n")
			jsonData, _ := opts.Marshal(m.KvClientMessage)
			fmt.Fprintf(outFile, "Content:\n%s\n", string(jsonData))

		case *agentv1.AgentClientMessage_ClientHeartbeat:
			fmt.Fprintf(outFile, "Message Type: clientHeartbeat\n")
			jsonData, _ := opts.Marshal(m.ClientHeartbeat)
			fmt.Fprintf(outFile, "Content:\n%s\n", string(jsonData))

		case *agentv1.AgentClientMessage_InteractionResponse:
			fmt.Fprintf(outFile, "Message Type: interactionResponse\n")
			if m.InteractionResponse != nil {
				actionJSON, _ := opts.Marshal(m.InteractionResponse)
				fmt.Fprintf(outFile, "Content:\n%s\n", string(actionJSON))
			}

		case *agentv1.AgentClientMessage_ExecClientControlMessage:
			fmt.Fprintf(outFile, "Message Type: execClientControlMessage\n")
			jsonData, _ := opts.Marshal(m.ExecClientControlMessage)
			fmt.Fprintf(outFile, "Content:\n%s\n", string(jsonData))

		case *agentv1.AgentClientMessage_PrewarmRequest:
			fmt.Fprintf(outFile, "Message Type: prewarmRequest\n")
			jsonData, _ := opts.Marshal(m.PrewarmRequest)
			fmt.Fprintf(outFile, "Content:\n%s\n", string(jsonData))

		default:
			fmt.Fprintf(outFile, "Message Type: unknown\n")
			jsonData, _ := opts.Marshal(&clientMsg)
			fmt.Fprintf(outFile, "Full message:\n%s\n", string(jsonData))
		}
	}

	fmt.Printf("Processed %d BidiAppend messages. Output in bidi_debug.txt\n", count)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
