package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// Gemini 3 (OpenAI-compatible endpoint) returns a per-tool-call thought_signature
// under tool_calls[].extra_content.google.thought_signature, and REQUIRES it to be
// replayed verbatim on the assistant turn of the next request; omitting it makes
// the API reject multi-turn tool calls with 400 "Function call is missing a
// thought_signature". These tests pin the capture (response -> block) and the
// replay (block -> request) so the round-trip cannot silently regress.

func TestParseOpenAIResponseCapturesThoughtSignature(t *testing.T) {
	raw := []byte(`{
	  "choices":[{"finish_reason":"tool_calls","index":0,"message":{"role":"assistant","tool_calls":[
	    {"id":"call_1","type":"function",
	     "extra_content":{"google":{"thought_signature":"SIG-ABC123"}},
	     "function":{"name":"probe","arguments":"{\"x\":1}"}}
	  ]}}],
	  "usage":{"prompt_tokens":10,"completion_tokens":2}
	}`)
	msg, stop, _, err := parseOpenAIResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if stop != "tool_use" {
		t.Fatalf("stop = %q, want tool_use", stop)
	}
	uses := msg.ToolUses()
	if len(uses) != 1 {
		t.Fatalf("tool uses = %d, want 1", len(uses))
	}
	if got := uses[0].ThoughtSignature; got != "SIG-ABC123" {
		t.Fatalf("ThoughtSignature = %q, want SIG-ABC123", got)
	}
}

func TestToOpenAIMessagesReplaysThoughtSignature(t *testing.T) {
	msgs := []Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{
			Type:             BlockToolUse,
			ID:               "call_1",
			Name:             "probe",
			Input:            json.RawMessage(`{"x":1}`),
			ThoughtSignature: "SIG-ABC123",
		}},
	}}
	out := toOpenAIMessages("", msgs)
	if len(out) != 1 || len(out[0].ToolCalls) != 1 {
		t.Fatalf("unexpected shape: %+v", out)
	}
	tc := out[0].ToolCalls[0]
	if tc.ExtraContent == nil || tc.ExtraContent.Google == nil {
		t.Fatalf("extra_content not replayed: %+v", tc)
	}
	if tc.ExtraContent.Google.ThoughtSignature != "SIG-ABC123" {
		t.Fatalf("replayed sig = %q, want SIG-ABC123", tc.ExtraContent.Google.ThoughtSignature)
	}
	// And it must serialize to the exact wire shape Gemini expects.
	b, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"extra_content":{"google":{"thought_signature":"SIG-ABC123"}}`) {
		t.Fatalf("wire shape wrong: %s", b)
	}
}

// A tool call with no signature (any non-Gemini provider) must not emit an empty
// extra_content envelope — that would be noise at best and rejected at worst.
func TestToOpenAIMessagesOmitsEmptyExtraContent(t *testing.T) {
	msgs := []Message{{
		Role:    RoleAssistant,
		Content: []ContentBlock{{Type: BlockToolUse, ID: "c", Name: "f", Input: json.RawMessage(`{}`)}},
	}}
	out := toOpenAIMessages("", msgs)
	tc := out[0].ToolCalls[0]
	if tc.ExtraContent != nil {
		t.Fatalf("extra_content should be nil, got %+v", tc.ExtraContent)
	}
	if b, _ := json.Marshal(tc); strings.Contains(string(b), "extra_content") {
		t.Fatalf("empty extra_content leaked to wire: %s", b)
	}
}

// Round-trip: a captured signature survives response-parse then request-build.
func TestThoughtSignatureRoundTrip(t *testing.T) {
	raw := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","extra_content":{"google":{"thought_signature":"RT-999"}},"function":{"name":"f","arguments":"{}"}}]}}]}`)
	msg, _, _, err := parseOpenAIResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := toOpenAIMessages("", []Message{msg})
	tc := out[0].ToolCalls[0]
	if tc.ExtraContent == nil || tc.ExtraContent.Google == nil || tc.ExtraContent.Google.ThoughtSignature != "RT-999" {
		t.Fatalf("round-trip lost signature: %+v", tc)
	}
}
