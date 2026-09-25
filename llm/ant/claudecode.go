package ant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	directsdk "go.aponeill.com/claude-directsdk"
	"shelley.exe.dev/llm"
	"shelley.exe.dev/models/modelsdev"
)

// ClaudeCodeTransport is the MessageOrigin transport of Claude Code responses.
const ClaudeCodeTransport = "claude-code"

// claudeCodeToolPrefix is how directsdk names host tools on the wire.
const claudeCodeToolPrefix = "mcp__host__"

// ClaudeCodeService serves Claude through the local Claude Code CLI on a
// Pro/Max subscription. Shelley keeps the agent loop; each Do is one upstream
// request, and native never runs a tool.
type ClaudeCodeService struct {
	Client *directsdk.Client
	Model  string // directsdk route, e.g. "claude-opus-5-5[1m]"
}

var _ llm.Service = (*ClaudeCodeService)(nil)

func (s *ClaudeCodeService) Provider() string       { return "anthropic" }
func (s *ClaudeCodeService) MaxImageDimension() int { return 2000 }
func (s *ClaudeCodeService) MaxImageBytes() int     { return 5 * 1024 * 1024 }
func (s *ClaudeCodeService) SupportsImages() bool   { return true }

func (s *ClaudeCodeService) apiModel() string {
	return strings.TrimSuffix(s.Model, "[1m]")
}

// SupportsReasoning is false for budget-thinking models: directsdk only
// speaks adaptive thinking.
func (s *ClaudeCodeService) SupportsReasoning() bool { return useAdaptiveThinking(s.apiModel()) }

func (s *ClaudeCodeService) SupportedReasoningLevels() []llm.ThinkingLevel {
	caps, _ := modelsdev.LookupReasoningCapabilities("", s.apiModel())
	return caps.Levels
}

func (s *ClaudeCodeService) DefaultReasoningLevel() string {
	if !s.SupportsReasoning() {
		return "off"
	}
	return "medium"
}

func (s *ClaudeCodeService) Do(ctx context.Context, r *llm.Request) (*llm.Response, error) {
	req, err := s.request(r)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	var resp *directsdk.Response
	idx, last := -1, ""
	stream := func(typ, text string) {
		if text == "" || r.OnStream == nil {
			return
		}
		if typ != last {
			idx, last = idx+1, typ
		}
		r.OnStream(llm.StreamDelta{Type: typ, Text: text, Index: idx})
	}
	for ev, err := range s.Client.Stream(ctx, req) {
		if ue := (*directsdk.UpstreamError)(nil); errors.As(err, &ue) {
			return nil, claudeCodeError{ue}
		}
		if err != nil {
			return nil, err
		}
		stream("thinking", ev.Thinking)
		stream("text", ev.Text)
		if ev.Response != nil {
			resp = ev.Response
		}
	}
	end := time.Now()
	out, err := claudeCodeResponse(resp)
	if err != nil {
		return nil, err
	}
	out.StartTime, out.EndTime = &start, &end
	return out, nil
}

func (s *ClaudeCodeService) request(r *llm.Request) (*directsdk.Request, error) {
	if r.ToolChoice != nil {
		return nil, errors.New("claude code: tool choice is unsupported")
	}
	req := &directsdk.Request{Model: s.Model}
	req.System = strings.Join(mapped(r.System, func(sc llm.SystemContent) string { return sc.Text }), "\n\n")
	for _, t := range r.Tools {
		if !t.ServerSide {
			req.Tools = append(req.Tools, directsdk.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
		}
	}
	for _, m := range r.Messages {
		// Server-side blocks (web search from an API-key turn) have no tool here.
		m.Content = slices.DeleteFunc(slices.Clone(m.Content), func(c llm.Content) bool { return llm.IsServerSideContentType(c.Type) })
		am := fromLLMMessage(m)
		if len(am.Content) == 0 {
			continue
		}
		dm := directsdk.Message{Role: am.Role}
		// Native replays thinking only when Model is the request's model.
		if m.Origin != nil && m.Origin.Transport == ClaudeCodeTransport {
			dm.Model = m.Origin.Model
		}
		for _, c := range am.Content {
			// Native places its own 1h breakpoints; a 5m one before them is a 400.
			c.CacheControl = nil
			if c.Type == "tool_use" {
				c.ToolName = claudeCodeToolPrefix + c.ToolName
			}
			b, err := json.Marshal(c)
			if err != nil {
				return nil, err
			}
			dm.Content = append(dm.Content, b)
		}
		req.Messages = append(req.Messages, dm)
	}
	if s.SupportsReasoning() {
		level := llm.EffectiveThinkingLevel(llm.ThinkingLevelMedium, r.ThinkingLevel)
		level = llm.ClampThinkingLevel(level, s.SupportedReasoningLevels())
		req.Thinking = new(level != llm.ThinkingLevelOff)
		if *req.Thinking {
			req.Effort = max(level, llm.ThinkingLevelLow).ThinkingEffort()
		}
	}
	return req, nil
}

func claudeCodeResponse(resp *directsdk.Response) (*llm.Response, error) {
	raw := response{Role: "assistant", Model: resp.Message.Model, StopReason: resp.StopReason}
	if err := json.Unmarshal(resp.Usage.Raw, &raw.Usage); err != nil {
		return nil, err
	}
	for _, b := range resp.Message.Content {
		var c content
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, err
		}
		if c.Type == "tool_use" {
			c.ToolName = strings.TrimPrefix(c.ToolName, claudeCodeToolPrefix)
		}
		raw.Content = append(raw.Content, c)
	}
	out := toLLMResponse(&raw)
	out.Origin = &llm.MessageOrigin{Provider: "anthropic", Transport: ClaudeCodeTransport, Model: resp.Message.Model}
	return out, nil
}

// claudeCodeError lets the loop retry transient upstream failures: native's
// own retries are off.
type claudeCodeError struct{ *directsdk.UpstreamError }

func (e claudeCodeError) Unwrap() error { return e.UpstreamError }

func (e claudeCodeError) RequestErrorInfo() llm.RequestErrorInfo {
	s := e.Status
	return llm.RequestErrorInfo{Retryable: s == 0 || s == 429 || s >= 500}
}
