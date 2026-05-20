package main

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	agentv1 "github.com/burpheart/cursor-tap/cursor_proto/gen/agent/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// LogEntry represents a single JSONL record
type LogEntry struct {
	Ts          string `json:"ts"`
	Session     string `json:"session"`
	Seq         int    `json:"seq"`
	Index       int    `json:"index"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	Direction   string `json:"direction"`
	GrpcService string `json:"grpc_service"`
	GrpcMethod  string `json:"grpc_method"`
	GrpcData    string `json:"grpc_data"`
	GrpcRaw     string `json:"grpc_raw"`
}

// BidiAppendData represents the JSON structure inside grpc_data for BidiAppend
type BidiAppendData struct {
	Data      string `json:"data"`
	RequestId struct {
		RequestId string `json:"requestId"`
	} `json:"requestId"`
	AppendSeqno json.Number `json:"appendSeqno"`
}

// RawMessage for sorting
type RawMessage struct {
	Timestamp   string
	Direction   string
	MessageType string
	Content     string
	ToolCallId  string
	MessageId   string // For deduplication of user messages
}

// ConversationBubble represents a complete dialog bubble
type ConversationBubble struct {
	Timestamp string
	Role      string // user, assistant, tool, system
	Type      string // text, thinking, tool_call, tool_result, exec
	Content   string
	ToolInfo  *ToolInfo
}

type ToolInfo struct {
	CallId  string
	Name    string
	Path    string
	Command string
	Result  string
}

var outFile *os.File
var htmlMode bool

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: restore <jsonl_file> [request_id] [output_file]")
		fmt.Println("  If output_file is not specified, defaults to conversation_<request_id>.txt")
		fmt.Println("  Use .html extension for HTML output")
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	var filterRequestId string
	if len(os.Args) > 2 {
		filterRequestId = os.Args[2]
	}

	// Setup output file
	outFile = os.Stdout // default

	// Collect all messages
	var messages []RawMessage
	requestIds := make(map[string]int)

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 500*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Type != "grpc" || entry.GrpcData == "" {
			continue
		}

		// Process BidiAppend (C2S)
		if entry.GrpcMethod == "BidiAppend" && entry.Direction == "C2S" {
			msg := processBidiAppend(entry)
			if msg != nil {
				if filterRequestId == "" || msg.RequestId == filterRequestId {
					messages = append(messages, RawMessage{
						Timestamp:   entry.Ts,
						Direction:   "C2S",
						MessageType: msg.MessageType,
						Content:     msg.Content,
					})
					requestIds[msg.RequestId]++
				}
			}
		}

		// Process RunSSE (S2C)
		if entry.GrpcMethod == "RunSSE" && entry.Direction == "S2C" {
			msg := processRunSSE(entry)
			if msg != nil {
				messages = append(messages, *msg)
				requestIds[filterRequestId]++
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
	}

	// If no filter specified, show available request IDs
	if filterRequestId == "" {
		fmt.Println("Available request IDs (sorted by message count):")
		type kv struct {
			Key   string
			Value int
		}
		var sorted []kv
		for k, v := range requestIds {
			sorted = append(sorted, kv{k, v})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Value > sorted[j].Value
		})
		for i, kv := range sorted {
			if i >= 20 {
				break
			}
			fmt.Printf("  %s: %d messages\n", kv.Key, kv.Value)
		}
		fmt.Printf("\nTotal: %d request IDs, %d messages\n", len(requestIds), len(messages))
		return
	}

	// Create output file
	outputPath := ""
	if len(os.Args) > 3 {
		outputPath = os.Args[3]
	} else {
		// Default output file name
		shortId := filterRequestId
		if len(shortId) > 8 {
			shortId = shortId[:8]
		}
		outputPath = fmt.Sprintf("conversation_%s.txt", shortId)
	}

	// Check if HTML mode
	htmlMode = strings.HasSuffix(strings.ToLower(outputPath), ".html")

	outFile, err = os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()
	fmt.Printf("Writing to: %s (HTML: %v)\n", outputPath, htmlMode)

	// Sort by timestamp
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})

	// Merge streams into bubbles
	bubbles := mergeIntoBubbles(messages)

	// Count statistics
	stats := make(map[string]int)
	for _, b := range bubbles {
		key := b.Role + ":" + b.Type
		stats[key]++
		stats["role:"+b.Role]++
		stats["type:"+b.Type]++
	}

	// Output conversation
	if htmlMode {
		writeHTMLHeader(filterRequestId, len(bubbles), len(messages), stats)
		for i, bubble := range bubbles {
			writeHTMLBubble(i+1, bubble)
		}
		writeHTMLFooter()
	} else {
		output("=== Conversation: %s ===\n", filterRequestId)
		output("Total bubbles: %d (from %d raw messages)\n\n", len(bubbles), len(messages))
		for i, bubble := range bubbles {
			printBubble(i+1, bubble)
		}
	}

	fmt.Printf("Done. %d bubbles written.\n", len(bubbles))
}

func output(format string, args ...interface{}) {
	fmt.Fprintf(outFile, format, args...)
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func writeHTMLHeader(requestId string, bubbleCount, messageCount int, stats map[string]int) {
	output(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Conversation: %s</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { 
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: #1a1a2e; color: #eee; line-height: 1.6; padding: 20px;
}
.container { max-width: 900px; margin: 0 auto; }
.header { text-align: center; padding: 20px 0; border-bottom: 1px solid #333; margin-bottom: 20px; }
.header h1 { font-size: 1.5em; color: #7b68ee; }
.header p { color: #888; font-size: 0.9em; margin-top: 5px; }
.stats { display: flex; flex-wrap: wrap; gap: 10px; justify-content: center; margin: 15px 0; }
.stat-item { background: #252538; padding: 8px 15px; border-radius: 8px; font-size: 0.85em; }
.stat-item .label { color: #888; }
.stat-item .count { font-weight: bold; margin-left: 5px; }
.filters { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; margin: 15px 0; padding: 15px; background: #252538; border-radius: 10px; }
.filter-btn { padding: 6px 14px; border: none; border-radius: 6px; cursor: pointer; font-size: 0.85em; transition: all 0.2s; }
.filter-btn.active { opacity: 1; }
.filter-btn.inactive { opacity: 0.4; }
.filter-btn.role-user { background: #4299e1; color: white; }
.filter-btn.role-assistant { background: #68d391; color: #1a1a2e; }
.filter-btn.role-tool { background: #ed8936; color: white; }
.filter-btn.role-system { background: #718096; color: white; }
.filter-btn.type-text { background: #63b3ed; color: #1a1a2e; }
.filter-btn.type-thinking { background: #9f7aea; color: white; }
.filter-btn.type-tool_call { background: #f6ad55; color: #1a1a2e; }
.filter-btn.type-tool_result { background: #fc8181; color: white; }
.filter-btn.type-exec { background: #a0aec0; color: #1a1a2e; }
.filter-btn:hover { transform: scale(1.05); }
.filter-section { margin: 5px 0; }
.filter-label { color: #888; font-size: 0.8em; margin-right: 10px; }
.bubble { margin: 15px 0; padding: 15px; border-radius: 12px; position: relative; }
.bubble.hidden { display: none; }
.bubble.user { background: #2d3748; margin-left: 50px; border-left: 4px solid #4299e1; }
.bubble.assistant { background: #1e3a5f; margin-right: 50px; border-left: 4px solid #68d391; }
.bubble.assistant.thinking { background: #2d2d44; border-left-color: #9f7aea; opacity: 0.9; }
.bubble.tool { background: #3d2d1f; margin-left: 100px; border-left: 4px solid #ed8936; font-size: 0.9em; }
.bubble.system { background: #2d2d2d; margin: 10px 100px; border-left: 4px solid #718096; font-size: 0.85em; }
.bubble.separator { background: transparent; text-align: center; color: #666; border: none; padding: 5px; }
.meta { display: flex; justify-content: space-between; margin-bottom: 10px; font-size: 0.8em; color: #888; }
.role { font-weight: bold; text-transform: uppercase; }
.role.user { color: #4299e1; }
.role.assistant { color: #68d391; }
.role.tool { color: #ed8936; }
.role.system { color: #718096; }
.type { background: #333; padding: 2px 8px; border-radius: 4px; }
.tool-info { background: #222; padding: 8px 12px; border-radius: 6px; margin-bottom: 10px; font-size: 0.85em; color: #aaa; }
.content { white-space: pre-wrap; word-break: break-word; }
.content code { background: #0d1117; padding: 2px 6px; border-radius: 4px; font-family: 'Fira Code', monospace; }
pre.code-block { background: #0d1117; padding: 15px; border-radius: 8px; overflow-x: auto; margin: 10px 0; font-size: 0.85em; }
.timestamp { font-family: monospace; }
.index { background: #333; color: #888; padding: 2px 8px; border-radius: 4px; font-size: 0.75em; }
.visible-count { text-align: center; color: #888; font-size: 0.9em; margin: 10px 0; }
</style>
</head>
<body>
<div class="container">
<div class="header">
<h1>Conversation Replay</h1>
<p>Request ID: %s</p>
<p>%d bubbles from %d raw messages</p>

<div class="stats">
`, requestId, requestId, bubbleCount, messageCount)

	// Output role stats
	roles := []string{"user", "assistant", "tool", "system"}
	for _, role := range roles {
		if count, ok := stats["role:"+role]; ok && count > 0 {
			output(`<div class="stat-item"><span class="label">%s:</span><span class="count">%d</span></div>`, strings.ToUpper(role), count)
		}
	}
	output(`</div>
<div class="stats">
`)
	// Output type stats
	types := []string{"text", "thinking", "tool_call", "tool_result", "exec"}
	for _, t := range types {
		if count, ok := stats["type:"+t]; ok && count > 0 {
			output(`<div class="stat-item"><span class="label">%s:</span><span class="count">%d</span></div>`, t, count)
		}
	}
	output(`</div>

<div class="filters">
<div class="filter-section">
<span class="filter-label">Role:</span>
`)
	// Role filter buttons
	for _, role := range roles {
		if count, ok := stats["role:"+role]; ok && count > 0 {
			output(`<button class="filter-btn role-%s active" data-filter="role" data-value="%s" onclick="toggleFilter(this)">%s (%d)</button>`, role, role, strings.ToUpper(role), count)
		}
	}
	output(`</div>
<div class="filter-section">
<span class="filter-label">Type:</span>
`)
	// Type filter buttons
	for _, t := range types {
		if count, ok := stats["type:"+t]; ok && count > 0 {
			output(`<button class="filter-btn type-%s active" data-filter="type" data-value="%s" onclick="toggleFilter(this)">%s (%d)</button>`, strings.ReplaceAll(t, "_", "_"), t, t, count)
		}
	}
	output(`</div>
</div>
<div class="visible-count" id="visibleCount">Showing all %d bubbles</div>
</div>
<div id="bubbles">
`, bubbleCount)
}

func writeHTMLBubble(index int, bubble ConversationBubble) {
	bubbleClass := bubble.Role
	if bubble.Type == "thinking" {
		bubbleClass += " thinking"
	}
	if bubble.Type == "separator" {
		output(`<div class="bubble separator" data-role="separator" data-type="separator">%s</div>`+"\n", escapeHTML(bubble.Content))
		return
	}

	ts := bubble.Timestamp
	if len(ts) > 19 {
		ts = ts[:19]
	}

	output(`<div class="bubble %s" data-role="%s" data-type="%s">
<div class="meta">
<span><span class="index">#%d</span> <span class="role %s">%s</span> <span class="type">%s</span></span>
<span class="timestamp">%s</span>
</div>
`, bubbleClass, bubble.Role, bubble.Type, index, bubble.Role, strings.ToUpper(bubble.Role), bubble.Type, ts)

	if bubble.ToolInfo != nil && bubble.ToolInfo.Name != "" {
		output(`<div class="tool-info">`)
		output(`<strong>Tool:</strong> %s`, escapeHTML(bubble.ToolInfo.Name))
		if bubble.ToolInfo.Path != "" {
			output(` | <strong>Path:</strong> %s`, escapeHTML(bubble.ToolInfo.Path))
		}
		if bubble.ToolInfo.Command != "" {
			output(` | <strong>Cmd:</strong> %s`, escapeHTML(bubble.ToolInfo.Command))
		}
		output(`</div>` + "\n")
	}

	if bubble.Content != "" {
		content := escapeHTML(bubble.Content)
		// Convert markdown code blocks to HTML
		content = convertCodeBlocks(content)
		output(`<div class="content">%s</div>`+"\n", content)
	}

	output(`</div>` + "\n")
}

func convertCodeBlocks(content string) string {
	// Simple conversion of ```...``` to <pre class="code-block">...</pre>
	lines := strings.Split(content, "\n")
	var result []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCodeBlock {
				result = append(result, `</pre>`)
				inCodeBlock = false
			} else {
				result = append(result, `<pre class="code-block">`)
				inCodeBlock = true
			}
		} else {
			result = append(result, line)
		}
	}

	if inCodeBlock {
		result = append(result, `</pre>`)
	}

	return strings.Join(result, "\n")
}

func writeHTMLFooter() {
	output(`</div>
</div>
<script>
const filters = {
  role: new Set(['user', 'assistant', 'tool', 'system']),
  type: new Set(['text', 'thinking', 'tool_call', 'tool_result', 'exec'])
};

function toggleFilter(btn) {
  const filterType = btn.dataset.filter;
  const value = btn.dataset.value;
  
  if (filters[filterType].has(value)) {
    filters[filterType].delete(value);
    btn.classList.remove('active');
    btn.classList.add('inactive');
  } else {
    filters[filterType].add(value);
    btn.classList.add('active');
    btn.classList.remove('inactive');
  }
  
  applyFilters();
}

function applyFilters() {
  const bubbles = document.querySelectorAll('#bubbles .bubble');
  let visibleCount = 0;
  
  bubbles.forEach(bubble => {
    const role = bubble.dataset.role;
    const type = bubble.dataset.type;
    
    const roleMatch = filters.role.has(role) || role === 'separator';
    const typeMatch = filters.type.has(type) || type === 'separator';
    
    if (roleMatch && typeMatch) {
      bubble.classList.remove('hidden');
      visibleCount++;
    } else {
      bubble.classList.add('hidden');
    }
  });
  
  document.getElementById('visibleCount').textContent = 
    'Showing ' + visibleCount + ' of ' + bubbles.length + ' bubbles';
}
</script>
</body>
</html>
`)
}

func mergeIntoBubbles(messages []RawMessage) []ConversationBubble {
	var bubbles []ConversationBubble

	var currentThinking strings.Builder
	var currentText strings.Builder
	var currentToolDeltas = make(map[string]*strings.Builder) // callId -> content
	var thinkingStart, textStart string
	var pendingToolCalls = make(map[string]*ToolInfo)
	var seenUserMessages = make(map[string]bool) // For deduplication of user messages

	flushThinking := func() {
		if currentThinking.Len() > 0 {
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: thinkingStart,
				Role:      "assistant",
				Type:      "thinking",
				Content:   currentThinking.String(),
			})
			currentThinking.Reset()
		}
	}

	flushText := func() {
		if currentText.Len() > 0 {
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: textStart,
				Role:      "assistant",
				Type:      "text",
				Content:   currentText.String(),
			})
			currentText.Reset()
		}
	}

	flushToolDelta := func(callId string) {
		if builder, ok := currentToolDeltas[callId]; ok && builder.Len() > 0 {
			toolInfo := pendingToolCalls[callId]
			if toolInfo == nil {
				toolInfo = &ToolInfo{CallId: callId}
			}
			bubbles = append(bubbles, ConversationBubble{
				Role:     "assistant",
				Type:     "tool_call",
				Content:  builder.String(),
				ToolInfo: toolInfo,
			})
			delete(currentToolDeltas, callId)
		}
	}

	for _, msg := range messages {
		switch {
		case msg.MessageType == "thinkingDelta":
			if currentThinking.Len() == 0 {
				thinkingStart = msg.Timestamp
			}
			currentThinking.WriteString(msg.Content)

		case msg.MessageType == "thinkingCompleted":
			flushThinking()

		case msg.MessageType == "textDelta":
			if currentText.Len() == 0 {
				textStart = msg.Timestamp
			}
			currentText.WriteString(msg.Content)

		case strings.HasPrefix(msg.MessageType, "partialToolCall:"):
			// Start of tool call, extract tool info
			toolType := strings.TrimPrefix(msg.MessageType, "partialToolCall:")
			info := parseToolInfo(msg.Content, toolType)
			if info.CallId != "" {
				pendingToolCalls[info.CallId] = info
			}

		case strings.HasPrefix(msg.MessageType, "toolCallDelta:"):
			// Accumulate tool call content
			callId := msg.ToolCallId
			if callId == "" {
				// Try to find from pending
				for id := range pendingToolCalls {
					callId = id
					break
				}
			}
			if callId != "" {
				if _, ok := currentToolDeltas[callId]; !ok {
					currentToolDeltas[callId] = &strings.Builder{}
				}
				currentToolDeltas[callId].WriteString(msg.Content)
			}

		case msg.MessageType == "toolCallStarted":
			flushText() // Text before tool call
			info := parseToolStarted(msg.Content)
			if info.CallId != "" {
				pendingToolCalls[info.CallId] = info
			}

		case msg.MessageType == "toolCallCompleted":
			info := parseToolCompleted(msg.Content)
			if info.CallId != "" {
				flushToolDelta(info.CallId)
				delete(pendingToolCalls, info.CallId)
			}

		case strings.HasPrefix(msg.MessageType, "ExecServer:"):
			// Exec request from server
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      "system",
				Type:      "exec",
				Content:   fmt.Sprintf("[%s] %s", msg.MessageType, msg.Content),
			})

		case msg.MessageType == "RunRequest:UserMessage":
			flushThinking()
			flushText()
			// User message
			content := msg.Content
			// Try to extract actual user query from JSON
			if extracted := extractUserQuery(content); extracted != "" {
				content = extracted
			}
			// Deduplicate by content
			if !seenUserMessages[content] {
				seenUserMessages[content] = true
				bubbles = append(bubbles, ConversationBubble{
					Timestamp: msg.Timestamp,
					Role:      "user",
					Type:      "text",
					Content:   content,
				})
			}

		case msg.MessageType == "ConversationAction":
			if msg.Content != "" {
				// Deduplicate by content
				if !seenUserMessages[msg.Content] {
					seenUserMessages[msg.Content] = true
					bubbles = append(bubbles, ConversationBubble{
						Timestamp: msg.Timestamp,
						Role:      "user",
						Type:      "text",
						Content:   msg.Content,
					})
				}
			}

		case msg.MessageType == "userMessageAppended":
			// User message echoed from S2C stream - skip to avoid duplicates
			// (already captured from C2S RunRequest:UserMessage or ConversationAction)

		case msg.MessageType == "turnEnded":
			flushThinking()
			flushText()
			for callId := range currentToolDeltas {
				flushToolDelta(callId)
			}
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      "system",
				Type:      "separator",
				Content:   "--- Turn End ---",
			})

		case msg.MessageType == "ConversationCheckpoint":
			// Skip checkpoints in bubble view

		// Skip internal/metadata message types (no content value)
		case msg.MessageType == "token_delta",
			msg.MessageType == "heartbeat",
			msg.MessageType == "step_completed",
			msg.MessageType == "step_started",
			msg.MessageType == "Heartbeat",
			msg.MessageType == "ServerHeartbeat",
			msg.MessageType == "nil",
			msg.MessageType == "summaryStarted":
			// Skip these metadata/internal messages

		case msg.MessageType == "summaryCompleted":
			// Summary completed - optionally show hook message
			if msg.Content != "" {
				bubbles = append(bubbles, ConversationBubble{
					Timestamp: msg.Timestamp,
					Role:      "system",
					Type:      "summary",
					Content:   msg.Content,
				})
			}

		case msg.MessageType == "summary":
			// Conversation summary
			if msg.Content != "" {
				bubbles = append(bubbles, ConversationBubble{
					Timestamp: msg.Timestamp,
					Role:      "system",
					Type:      "summary",
					Content:   msg.Content,
				})
			}

		case strings.HasPrefix(msg.MessageType, "KvServer:"):
			// KV request from server
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      "system",
				Type:      "kv_request",
				Content:   fmt.Sprintf("[%s] %s", msg.MessageType, msg.Content),
			})

		case strings.HasPrefix(msg.MessageType, "KvClient:"):
			// KV response from client
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      "system",
				Type:      "kv_response",
				Content:   fmt.Sprintf("[%s] %s", msg.MessageType, msg.Content),
			})

		case strings.HasPrefix(msg.MessageType, "interactionQuery:"):
			// Interaction query from server (ask_question, etc.)
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      "system",
				Type:      "query",
				Content:   fmt.Sprintf("[%s] %s", msg.MessageType, msg.Content),
			})

		case msg.MessageType == "ExecServerControlMessage",
			msg.MessageType == "ExecClientControlMessage":
			// Exec control messages (stream close, etc.) - skip unless debugging

		case msg.Direction == "C2S" && strings.Contains(msg.MessageType, "ExecClientMessage"):
			// Tool execution result from client
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      "tool",
				Type:      "tool_result",
				Content:   msg.Content,
			})

		default:
			// Unknown message type - log warning and include in output
			fmt.Fprintf(os.Stderr, "[WARN] Unknown message type: %s (direction: %s)\n", msg.MessageType, msg.Direction)
			role := "system"
			if msg.Direction == "C2S" {
				role = "client"
			} else if msg.Direction == "S2C" {
				role = "server"
			}
			bubbles = append(bubbles, ConversationBubble{
				Timestamp: msg.Timestamp,
				Role:      role,
				Type:      msg.MessageType,
				Content:   msg.Content,
			})
		}
	}

	// Flush remaining
	flushThinking()
	flushText()
	for callId := range currentToolDeltas {
		flushToolDelta(callId)
	}

	return bubbles
}

func parseToolInfo(content, toolType string) *ToolInfo {
	info := &ToolInfo{Name: toolType}
	// Try to parse path from content like "path: xxx"
	if strings.HasPrefix(content, "path: ") {
		info.Path = strings.TrimPrefix(content, "path: ")
	} else if strings.HasPrefix(content, "cmd: ") {
		info.Command = strings.TrimPrefix(content, "cmd: ")
	}
	return info
}

func parseToolStarted(content string) *ToolInfo {
	info := &ToolInfo{}
	var data map[string]interface{}
	if json.Unmarshal([]byte(content), &data) == nil {
		if id, ok := data["callId"].(string); ok {
			info.CallId = id
		}
		if t, ok := data["type"].(string); ok {
			info.Name = t
		}
		if p, ok := data["path"].(string); ok {
			info.Path = p
		}
		if c, ok := data["command"].(string); ok {
			info.Command = c
		}
	}
	return info
}

func parseToolCompleted(content string) *ToolInfo {
	info := &ToolInfo{}
	var data map[string]interface{}
	if json.Unmarshal([]byte(content), &data) == nil {
		if id, ok := data["callId"].(string); ok {
			info.CallId = id
		}
	}
	return info
}

func extractUserQuery(content string) string {
	// Try to find <user_query> tag
	if idx := strings.Index(content, "<user_query>"); idx >= 0 {
		start := idx + len("<user_query>")
		if end := strings.Index(content[start:], "</user_query>"); end >= 0 {
			return strings.TrimSpace(content[start : start+end])
		}
	}
	return content
}

func printBubble(index int, bubble ConversationBubble) {
	roleLabel := map[string]string{
		"user":      "[USER]",
		"assistant": "[ASSISTANT]",
		"tool":      "[TOOL]",
		"system":    "[SYSTEM]",
	}

	label := roleLabel[bubble.Role]
	if label == "" {
		label = "[?]"
	}

	ts := bubble.Timestamp
	if len(ts) > 19 {
		ts = ts[:19]
	}

	output("[%d] %s %s (%s)\n", index, ts, label, bubble.Type)

	if bubble.ToolInfo != nil && bubble.ToolInfo.Name != "" {
		output("    Tool: %s", bubble.ToolInfo.Name)
		if bubble.ToolInfo.Path != "" {
			output(" | Path: %s", bubble.ToolInfo.Path)
		}
		if bubble.ToolInfo.Command != "" {
			output(" | Cmd: %s", bubble.ToolInfo.Command)
		}
		output("\n")
	}

	if bubble.Content != "" {
		// Indent content
		lines := strings.Split(bubble.Content, "\n")
		maxLines := 100 // Limit lines per bubble
		for i, line := range lines {
			if i >= maxLines {
				output("    ... (%d more lines)\n", len(lines)-maxLines)
				break
			}
			if len(line) > 500 {
				output("    %s...\n", line[:500])
			} else {
				output("    %s\n", line)
			}
		}
	}
	output("\n")
}

// Message structures for C2S processing
type ParsedMessage struct {
	RequestId   string
	MessageType string
	Content     string
}

func processBidiAppend(entry LogEntry) *ParsedMessage {
	var bidiData BidiAppendData
	if err := json.Unmarshal([]byte(entry.GrpcData), &bidiData); err != nil {
		return nil
	}

	if bidiData.Data == "" {
		return nil
	}

	rawBytes, err := hex.DecodeString(bidiData.Data)
	if err != nil {
		return nil
	}

	var clientMsg agentv1.AgentClientMessage
	if err := proto.Unmarshal(rawBytes, &clientMsg); err != nil {
		return nil
	}

	msgType, content := extractClientMessageContent(&clientMsg)

	return &ParsedMessage{
		RequestId:   bidiData.RequestId.RequestId,
		MessageType: msgType,
		Content:     content,
	}
}

func extractClientMessageContent(msg *agentv1.AgentClientMessage) (string, string) {
	if msg == nil || msg.Message == nil {
		return "nil", ""
	}

	// Get the oneof field name using reflection
	ref := msg.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName("message")
	if oneofDesc == nil {
		fmt.Fprintf(os.Stderr, "[WARN] AgentClientMessage has no 'message' oneof\n")
		return "Unknown", protoToJSON(msg)
	}

	field := ref.WhichOneof(oneofDesc)
	if field == nil {
		return "Empty", ""
	}

	msgType := string(field.Name())
	fieldValue := ref.Get(field)

	// For specific types, extract user-friendly content
	switch msgType {
	case "run_request":
		if req, ok := fieldValue.Message().Interface().(*agentv1.AgentRunRequest); ok {
			return extractRunRequestContent(req)
		}
	case "conversation_action":
		if action, ok := fieldValue.Message().Interface().(*agentv1.ConversationAction); ok {
			content := extractConversationActionContent(action)
			if content != "" {
				return "ConversationAction", content
			}
		}
	case "exec_client_message":
		if execMsg, ok := fieldValue.Message().Interface().(*agentv1.ExecClientMessage); ok {
			return "ExecClientMessage", extractExecClientContent(execMsg)
		}
	case "client_heartbeat":
		return "Heartbeat", ""
	case "kv_client_message":
		if kvm, ok := fieldValue.Message().Interface().(*agentv1.KvClientMessage); ok {
			return extractKvClientContent(kvm)
		}
		return "KvClientMessage", protoToJSON(fieldValue.Message().Interface().(proto.Message))
	case "exec_client_control_message":
		return "ExecClientControlMessage", protoToJSON(fieldValue.Message().Interface().(proto.Message))
	case "interaction_response":
		return "InteractionResponse", protoToJSON(fieldValue.Message().Interface().(proto.Message))
	case "prewarm_request":
		return "PrewarmRequest", protoToJSON(fieldValue.Message().Interface().(proto.Message))
	default:
		// Unknown type - log warning
		fmt.Fprintf(os.Stderr, "[WARN] Unknown AgentClientMessage type: %s\n", msgType)
	}

	// Default: serialize the entire field as JSON
	if fieldValue.Message().IsValid() {
		return msgType, protoToJSON(fieldValue.Message().Interface().(proto.Message))
	}
	return msgType, ""
}

func protoToJSON(msg proto.Message) string {
	if msg == nil {
		return ""
	}
	opts := protojson.MarshalOptions{
		EmitUnpopulated: false,
		Indent:          "",
	}
	data, err := opts.Marshal(msg)
	if err != nil {
		return fmt.Sprintf("[marshal error: %v]", err)
	}
	return string(data)
}

func getOneofFieldName(msg proto.Message, oneofName string) string {
	if msg == nil {
		return ""
	}
	ref := msg.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName(protoreflect.Name(oneofName))
	if oneofDesc == nil {
		return ""
	}
	field := ref.WhichOneof(oneofDesc)
	if field == nil {
		return ""
	}
	return string(field.Name())
}

func extractRunRequestContent(req *agentv1.AgentRunRequest) (string, string) {
	if req == nil {
		return "RunRequest(nil)", ""
	}
	if req.Action != nil {
		actionType, content := extractConversationActionType(req.Action)
		if content != "" {
			return "RunRequest:" + actionType, content
		}
		return "RunRequest:" + actionType, protoToJSON(req.Action)
	}
	return "RunRequest", protoToJSON(req)
}

func extractConversationActionType(action *agentv1.ConversationAction) (string, string) {
	if action == nil || action.Action == nil {
		return "nil", ""
	}

	ref := action.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName("action")
	if oneofDesc == nil {
		fmt.Fprintf(os.Stderr, "[WARN] ConversationAction has no 'action' oneof\n")
		return "Unknown", ""
	}

	field := ref.WhichOneof(oneofDesc)
	if field == nil {
		return "Empty", ""
	}

	actionType := string(field.Name())

	// Extract user message text if available
	if actionType == "user_message_action" {
		if uma, ok := action.Action.(*agentv1.ConversationAction_UserMessageAction); ok {
			if uma.UserMessageAction != nil && uma.UserMessageAction.UserMessage != nil {
				return "UserMessage", uma.UserMessageAction.UserMessage.Text
			}
		}
	}

	return actionType, ""
}

func extractConversationActionContent(action *agentv1.ConversationAction) string {
	if action == nil || action.Action == nil {
		return ""
	}

	// Extract user message text if it's a user message action
	if uma, ok := action.Action.(*agentv1.ConversationAction_UserMessageAction); ok {
		if uma.UserMessageAction != nil && uma.UserMessageAction.UserMessage != nil {
			return uma.UserMessageAction.UserMessage.Text
		}
	}

	// For other action types, serialize to JSON
	return protoToJSON(action)
}

func extractExecClientContent(msg *agentv1.ExecClientMessage) string {
	if msg == nil {
		return ""
	}

	// Use reflection to get the oneof field type
	ref := msg.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName("message")

	result := make(map[string]interface{})
	result["id"] = msg.Id
	result["execId"] = msg.ExecId

	if oneofDesc == nil {
		fmt.Fprintf(os.Stderr, "[WARN] ExecClientMessage has no 'message' oneof\n")
		result["type"] = "Unknown"
		jsonBytes, _ := json.Marshal(result)
		return string(jsonBytes)
	}

	field := ref.WhichOneof(oneofDesc)
	if field == nil {
		result["type"] = "Empty"
		jsonBytes, _ := json.Marshal(result)
		return string(jsonBytes)
	}

	result["type"] = string(field.Name())

	// Serialize the oneof field content
	fieldValue := ref.Get(field)
	if fieldValue.Message().IsValid() {
		innerJSON := protoToJSON(fieldValue.Message().Interface().(proto.Message))
		var innerData map[string]interface{}
		if err := json.Unmarshal([]byte(innerJSON), &innerData); err == nil {
			for k, v := range innerData {
				result[k] = v
			}
		} else {
			result["data"] = innerJSON
		}
	}

	jsonBytes, _ := json.Marshal(result)
	return string(jsonBytes)
}

func processRunSSE(entry LogEntry) *RawMessage {
	var serverMsg agentv1.AgentServerMessage
	if err := protojson.Unmarshal([]byte(entry.GrpcData), &serverMsg); err != nil {
		return nil
	}

	msgType, content, toolCallId := extractServerMessageContent(&serverMsg)

	return &RawMessage{
		Timestamp:   entry.Ts,
		Direction:   "S2C",
		MessageType: msgType,
		Content:     content,
		ToolCallId:  toolCallId,
	}
}

func extractServerMessageContent(msg *agentv1.AgentServerMessage) (string, string, string) {
	if msg == nil || msg.Message == nil {
		return "nil", "", ""
	}

	ref := msg.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName("message")
	if oneofDesc == nil {
		fmt.Fprintf(os.Stderr, "[WARN] AgentServerMessage has no 'message' oneof\n")
		return "Unknown", protoToJSON(msg), ""
	}

	field := ref.WhichOneof(oneofDesc)
	if field == nil {
		return "Empty", "", ""
	}

	msgType := string(field.Name())
	fieldValue := ref.Get(field)

	// Handle specific message types
	switch msgType {
	case "interaction_update":
		if iu, ok := fieldValue.Message().Interface().(*agentv1.InteractionUpdate); ok {
			return extractInteractionContent(iu)
		}
	case "exec_server_message":
		if esm, ok := fieldValue.Message().Interface().(*agentv1.ExecServerMessage); ok {
			t, c := extractExecServerContent(esm)
			return t, c, ""
		}
	case "interaction_query":
		if iq, ok := fieldValue.Message().Interface().(*agentv1.InteractionQuery); ok {
			t, c := extractInteractionQueryContent(iq)
			return t, c, ""
		}
	case "conversation_checkpoint_update":
		return "ConversationCheckpoint", "", ""
	case "kv_server_message":
		if kvm, ok := fieldValue.Message().Interface().(*agentv1.KvServerMessage); ok {
			return extractKvServerContent(kvm)
		}
		return "KvServerMessage", protoToJSON(fieldValue.Message().Interface().(proto.Message)), ""
	case "server_heartbeat":
		return "ServerHeartbeat", "", ""
	case "exec_server_control_message":
		return "ExecServerControlMessage", protoToJSON(fieldValue.Message().Interface().(proto.Message)), ""
	default:
		// Unknown type - log warning
		fmt.Fprintf(os.Stderr, "[WARN] Unknown AgentServerMessage type: %s\n", msgType)
	}

	// Default: serialize the entire field
	if fieldValue.Message().IsValid() {
		return msgType, protoToJSON(fieldValue.Message().Interface().(proto.Message)), ""
	}
	return msgType, "", ""
}

func extractInteractionContent(msg *agentv1.InteractionUpdate) (string, string, string) {
	if msg == nil || msg.Message == nil {
		return "InteractionUpdate(nil)", "", ""
	}

	ref := msg.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName("message")
	if oneofDesc == nil {
		fmt.Fprintf(os.Stderr, "[WARN] InteractionUpdate has no 'message' oneof\n")
		return "Unknown", protoToJSON(msg), ""
	}

	field := ref.WhichOneof(oneofDesc)
	if field == nil {
		return "Empty", "", ""
	}

	msgType := string(field.Name())
	fieldValue := ref.Get(field)

	// Handle specific known types that need special extraction
	switch msgType {
	case "text_delta":
		if td, ok := fieldValue.Message().Interface().(*agentv1.TextDeltaUpdate); ok {
			return "textDelta", td.Text, ""
		}
	case "thinking_delta":
		if td, ok := fieldValue.Message().Interface().(*agentv1.ThinkingDeltaUpdate); ok {
			return "thinkingDelta", td.Text, ""
		}
	case "thinking_completed":
		return "thinkingCompleted", "", ""
	case "user_message_appended":
		if uma, ok := fieldValue.Message().Interface().(*agentv1.UserMessageAppendedUpdate); ok {
			if uma.UserMessage != nil {
				return "userMessageAppended", uma.UserMessage.Text, ""
			}
		}
		return "userMessageAppended", "", ""
	case "partial_tool_call":
		if ptc, ok := fieldValue.Message().Interface().(*agentv1.PartialToolCallUpdate); ok {
			return extractPartialToolCall(ptc)
		}
	case "tool_call_delta":
		if tcd, ok := fieldValue.Message().Interface().(*agentv1.ToolCallDeltaUpdate); ok {
			return extractToolCallDelta(tcd)
		}
	case "tool_call_started":
		if tcs, ok := fieldValue.Message().Interface().(*agentv1.ToolCallStartedUpdate); ok {
			return "toolCallStarted", extractToolCallStarted(tcs), ""
		}
	case "tool_call_completed":
		if tcc, ok := fieldValue.Message().Interface().(*agentv1.ToolCallCompletedUpdate); ok {
			return "toolCallCompleted", extractToolCallCompletedContent(tcc), ""
		}
	case "turn_ended":
		return "turnEnded", "", ""
	case "summary_started":
		return "summaryStarted", "", ""
	case "summary_completed":
		if sc, ok := fieldValue.Message().Interface().(*agentv1.SummaryCompletedUpdate); ok {
			if sc.HookMessage != nil {
				return "summaryCompleted", *sc.HookMessage, ""
			}
		}
		return "summaryCompleted", "", ""
	case "summary":
		if su, ok := fieldValue.Message().Interface().(*agentv1.SummaryUpdate); ok {
			return "summary", su.Summary, ""
		}
		return "summary", "", ""
	case "heartbeat", "token_delta", "step_completed", "step_started":
		return msgType, "", ""
	}

	// Default: serialize to JSON and log unknown type
	fmt.Fprintf(os.Stderr, "[INFO] InteractionUpdate type '%s' using default serialization\n", msgType)
	if fieldValue.Message().IsValid() {
		return msgType, protoToJSON(fieldValue.Message().Interface().(proto.Message)), ""
	}
	return msgType, "", ""
}

func extractPartialToolCall(msg *agentv1.PartialToolCallUpdate) (string, string, string) {
	if msg == nil || msg.ToolCall == nil {
		return "partialToolCall", "", ""
	}

	callId := msg.CallId
	toolType := getOneofFieldName(msg.ToolCall, "tool")
	if toolType == "" {
		toolType = "unknown"
		fmt.Fprintf(os.Stderr, "[WARN] PartialToolCall has unknown tool type\n")
	}

	return "partialToolCall:" + toolType, protoToJSON(msg.ToolCall), callId
}

func extractToolCallDelta(msg *agentv1.ToolCallDeltaUpdate) (string, string, string) {
	if msg == nil || msg.ToolCallDelta == nil {
		return "toolCallDelta", "", ""
	}

	callId := msg.CallId
	deltaType := getOneofFieldName(msg.ToolCallDelta, "delta")
	if deltaType == "" {
		deltaType = "unknown"
		fmt.Fprintf(os.Stderr, "[WARN] ToolCallDelta has unknown delta type\n")
	}

	return "toolCallDelta:" + deltaType, protoToJSON(msg.ToolCallDelta), callId
}

func extractToolCallStarted(msg *agentv1.ToolCallStartedUpdate) string {
	if msg == nil {
		return ""
	}
	result := make(map[string]interface{})
	result["callId"] = msg.CallId
	if msg.ToolCall != nil {
		result["toolType"] = getOneofFieldName(msg.ToolCall, "tool")
		result["toolCall"] = json.RawMessage(protoToJSON(msg.ToolCall))
	}
	jsonBytes, _ := json.Marshal(result)
	return string(jsonBytes)
}

func extractToolCallCompletedContent(msg *agentv1.ToolCallCompletedUpdate) string {
	if msg == nil {
		return ""
	}
	result := make(map[string]interface{})
	result["callId"] = msg.CallId
	if msg.ToolCall != nil {
		result["toolType"] = getOneofFieldName(msg.ToolCall, "tool")
		result["toolCall"] = json.RawMessage(protoToJSON(msg.ToolCall))
	}
	jsonBytes, _ := json.Marshal(result)
	return string(jsonBytes)
}

func extractInteractionQueryContent(msg *agentv1.InteractionQuery) (string, string) {
	if msg == nil {
		return "InteractionQuery", ""
	}
	queryType := getOneofFieldName(msg, "query")
	if queryType == "" {
		queryType = "unknown"
		fmt.Fprintf(os.Stderr, "[WARN] InteractionQuery has unknown query type\n")
	}
	return "interactionQuery:" + queryType, protoToJSON(msg)
}

func extractExecServerContent(msg *agentv1.ExecServerMessage) (string, string) {
	if msg == nil {
		return "ExecServerMessage(nil)", ""
	}

	msgType := getOneofFieldName(msg, "message")
	if msgType == "" {
		msgType = "unknown"
		fmt.Fprintf(os.Stderr, "[WARN] ExecServerMessage has unknown message type\n")
	}

	result := make(map[string]interface{})
	result["id"] = msg.Id
	result["execId"] = msg.ExecId
	result["type"] = msgType

	// Serialize the message content
	ref := msg.ProtoReflect()
	oneofDesc := ref.Descriptor().Oneofs().ByName("message")
	if oneofDesc != nil {
		field := ref.WhichOneof(oneofDesc)
		if field != nil {
			fieldValue := ref.Get(field)
			if fieldValue.Message().IsValid() {
				innerJSON := protoToJSON(fieldValue.Message().Interface().(proto.Message))
				var innerData map[string]interface{}
				if err := json.Unmarshal([]byte(innerJSON), &innerData); err == nil {
					for k, v := range innerData {
						result[k] = v
					}
				}
			}
		}
	}

	jsonBytes, _ := json.Marshal(result)
	return "ExecServer:" + msgType, string(jsonBytes)
}

func extractKvServerContent(msg *agentv1.KvServerMessage) (string, string, string) {
	if msg == nil {
		return "KvServerMessage(nil)", "", ""
	}

	result := make(map[string]interface{})
	result["id"] = msg.Id

	switch m := msg.Message.(type) {
	case *agentv1.KvServerMessage_GetBlobArgs:
		result["type"] = "GetBlobArgs"
		if m.GetBlobArgs != nil {
			// blob_id is bytes, encode as base64
			result["blobId"] = base64.StdEncoding.EncodeToString(m.GetBlobArgs.BlobId)
		}
		jsonBytes, _ := json.Marshal(result)
		return "KvServer:GetBlob", string(jsonBytes), ""

	case *agentv1.KvServerMessage_SetBlobArgs:
		result["type"] = "SetBlobArgs"
		if m.SetBlobArgs != nil {
			result["blobId"] = base64.StdEncoding.EncodeToString(m.SetBlobArgs.BlobId)
			// blob_data can be large, show size and preview
			dataLen := len(m.SetBlobArgs.BlobData)
			result["blobDataSize"] = dataLen
			if dataLen <= 200 {
				// Try to decode as UTF-8 string
				if utf8.Valid(m.SetBlobArgs.BlobData) {
					result["blobData"] = string(m.SetBlobArgs.BlobData)
				} else {
					result["blobData"] = base64.StdEncoding.EncodeToString(m.SetBlobArgs.BlobData)
				}
			} else {
				// Show preview
				preview := m.SetBlobArgs.BlobData[:100]
				if utf8.Valid(preview) {
					result["blobDataPreview"] = string(preview) + "..."
				} else {
					result["blobDataPreview"] = base64.StdEncoding.EncodeToString(preview) + "..."
				}
			}
		}
		jsonBytes, _ := json.Marshal(result)
		return "KvServer:SetBlob", string(jsonBytes), ""

	default:
		fmt.Fprintf(os.Stderr, "[WARN] Unknown KvServerMessage type\n")
		return "KvServerMessage", protoToJSON(msg), ""
	}
}

func extractKvClientContent(msg *agentv1.KvClientMessage) (string, string) {
	if msg == nil {
		return "KvClientMessage(nil)", ""
	}

	result := make(map[string]interface{})
	result["id"] = msg.Id

	switch m := msg.Message.(type) {
	case *agentv1.KvClientMessage_GetBlobResult:
		result["type"] = "GetBlobResult"
		if m.GetBlobResult != nil && m.GetBlobResult.BlobData != nil {
			dataLen := len(m.GetBlobResult.BlobData)
			result["blobDataSize"] = dataLen
			if dataLen <= 200 {
				if utf8.Valid(m.GetBlobResult.BlobData) {
					result["blobData"] = string(m.GetBlobResult.BlobData)
				} else {
					result["blobData"] = base64.StdEncoding.EncodeToString(m.GetBlobResult.BlobData)
				}
			} else {
				preview := m.GetBlobResult.BlobData[:100]
				if utf8.Valid(preview) {
					result["blobDataPreview"] = string(preview) + "..."
				} else {
					result["blobDataPreview"] = base64.StdEncoding.EncodeToString(preview) + "..."
				}
			}
		} else {
			result["blobData"] = nil
		}
		jsonBytes, _ := json.Marshal(result)
		return "KvClient:GetBlobResult", string(jsonBytes)

	case *agentv1.KvClientMessage_SetBlobResult:
		result["type"] = "SetBlobResult"
		if m.SetBlobResult != nil && m.SetBlobResult.Error != nil {
			result["error"] = protoToJSON(m.SetBlobResult.Error)
		} else {
			result["success"] = true
		}
		jsonBytes, _ := json.Marshal(result)
		return "KvClient:SetBlobResult", string(jsonBytes)

	default:
		fmt.Fprintf(os.Stderr, "[WARN] Unknown KvClientMessage type\n")
		return "KvClientMessage", protoToJSON(msg)
	}
}
