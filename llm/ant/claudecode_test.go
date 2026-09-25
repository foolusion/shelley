package ant

import (
	"encoding/json"
	"testing"

	directsdk "go.aponeill.com/claude-directsdk"
	"shelley.exe.dev/llm"
)

func TestClaudeCodeRoundTrip(t *testing.T) {
	s := &ClaudeCodeService{Model: "claude-opus-5-5[1m]"}
	origin := &llm.MessageOrigin{Provider: "anthropic", Transport: ClaudeCodeTransport, Model: Claude55Opus}
	r := &llm.Request{
		System: []llm.SystemContent{{Text: "a"}, {Text: "b"}},
		Tools:  []*llm.Tool{{Name: "bash", InputSchema: llm.EmptySchema()}, {Name: "web_search", ServerSide: true}},
		Messages: []llm.Message{
			{Role: llm.MessageRoleUser, Content: []llm.Content{{Type: llm.ContentTypeText, Text: "hi", Cache: true}}},
			{Role: llm.MessageRoleAssistant, Origin: origin, Content: []llm.Content{
				{Type: llm.ContentTypeServerToolUse, ID: "srv", ToolName: "web_search"},
				{Type: llm.ContentTypeToolUse, ID: "t1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
			}},
			{Role: llm.MessageRoleUser, Content: []llm.Content{{Type: llm.ContentTypeToolResult, ToolUseID: "t1", ToolResult: []llm.Content{llm.StringContent("ok")}}}},
		},
	}
	req, err := s.request(r)
	if err != nil {
		t.Fatal(err)
	}
	if req.System != "a\n\nb" || len(req.Tools) != 1 || len(req.Messages) != 3 || string(req.Messages[0].Content[0]) != `{"type":"text","text":"hi"}` {
		t.Fatalf("request = %+v", req)
	}
	if a := req.Messages[1]; a.Model != Claude55Opus || len(a.Content) != 1 || string(a.Content[0]) != `{"id":"t1","type":"tool_use","name":"mcp__host__bash","input":{}}` {
		t.Fatalf("assistant = %s %s", a.Model, a.Content)
	}
	if !*req.Thinking || req.Effort != "medium" {
		t.Fatalf("thinking = %v %q", *req.Thinking, req.Effort)
	}
	r.ThinkingLevel = llm.ThinkingLevelMax
	if req, _ = s.request(r); req.Effort != "max" {
		t.Fatalf("max: effort = %q", req.Effort)
	}

	resp, err := claudeCodeResponse(&directsdk.Response{
		Message: directsdk.Message{Role: "assistant", Model: Claude55Opus, Content: []json.RawMessage{
			directsdk.TextBlock("run it"), directsdk.ToolUseBlock("t2", "bash", map[string]string{}),
		}},
		StopReason: "tool_use",
		Usage:      directsdk.Usage{Raw: json.RawMessage(`{"input_tokens":3,"cache_read_input_tokens":5,"output_tokens":7}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != llm.StopReasonToolUse || resp.Content[1].ToolName != "bash" || resp.Content[0].Text != "run it" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.CacheReadInputTokens != 5 || resp.Usage.OutputTokens != 7 || !resp.Origin.Matches(*origin) {
		t.Fatalf("usage/origin = %+v %+v", resp.Usage, resp.Origin)
	}
}
