package main

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func text(s string) *schemas.ChatMessageContent { return &schemas.ChatMessageContent{ContentStr: &s} }

func system(s string) schemas.ChatMessage {
	return schemas.ChatMessage{Role: schemas.ChatMessageRoleSystem, Content: text(s)}
}

func user(s string) schemas.ChatMessage {
	return schemas.ChatMessage{Role: schemas.ChatMessageRoleUser, Content: text(s)}
}

func chatRequest(requestType schemas.RequestType, messages ...schemas.ChatMessage) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{
		RequestType: requestType,
		ChatRequest: &schemas.BifrostChatRequest{Model: "qwen3.8-27b", Input: messages},
	}
}

func roles(messages []schemas.ChatMessage) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = string(m.Role)
	}
	return out
}

func TestPreLLMHookMergesInjectedMCPInstructions(t *testing.T) {
	// The shape from production: Bifrost core prepends the MCP server instructions as its
	// own system message after the HTTP transport hooks have run, ahead of the client's.
	for _, requestType := range []schemas.RequestType{schemas.ChatCompletionRequest, schemas.ChatCompletionStreamRequest} {
		t.Run(string(requestType), func(t *testing.T) {
			name := "primary"
			first := system(`<mcp_server name="Keenable">...</mcp_server>`)
			first.Name = &name
			req := chatRequest(requestType,
				first,
				system(`MCP server "keenable" instructions: ...`),
				user("hi"),
				system("later"),
			)

			got, shortCircuit, err := PreLLMHook(nil, req)
			if err != nil || shortCircuit != nil {
				t.Fatalf("unexpected err=%v shortCircuit=%v", err, shortCircuit)
			}
			input := got.ChatRequest.Input
			if want := []string{"system", "user", "system"}; !equal(roles(input), want) {
				t.Fatalf("roles = %v, want %v", roles(input), want)
			}
			wantContent := "<mcp_server name=\"Keenable\">...</mcp_server>\n\nMCP server \"keenable\" instructions: ..."
			if c := input[0].Content; c == nil || c.ContentStr == nil || *c.ContentStr != wantContent {
				t.Fatalf("merged content = %#v, want %q", c, wantContent)
			}
			if input[0].Name == nil || *input[0].Name != "primary" {
				t.Fatalf("fields of the first system message were not preserved: %#v", input[0])
			}
			if *input[2].Content.ContentStr != "later" {
				t.Fatalf("non-leading system message changed: %q", *input[2].Content.ContentStr)
			}
		})
	}
}

func TestPreLLMHookDoesNotMutateCallerSlice(t *testing.T) {
	req := chatRequest(schemas.ChatCompletionRequest, system("A"), system("B"), user("hi"))
	original := req.ChatRequest.Input
	PreLLMHook(nil, req)
	if len(original) != 3 || *original[0].Content.ContentStr != "A" || *original[1].Content.ContentStr != "B" {
		t.Fatalf("original messages were modified: %v", roles(original))
	}
}

func TestPreLLMHookLeavesOtherRequestsUnchanged(t *testing.T) {
	blocks := schemas.ChatMessage{Role: schemas.ChatMessageRoleSystem, Content: &schemas.ChatMessageContent{
		ContentBlocks: []schemas.ChatContentBlock{{Type: schemas.ChatContentBlockTypeText, Text: new("B")}},
	}}
	tests := []struct {
		name string
		req  *schemas.BifrostRequest
	}{
		{"nil request", nil},
		{"no chat request", &schemas.BifrostRequest{RequestType: schemas.ChatCompletionRequest}},
		{"responses request", chatRequest(schemas.ResponsesRequest, system("A"), system("B"))},
		{"single system message", chatRequest(schemas.ChatCompletionRequest, system("A"), user("hi"))},
		{"system messages not leading", chatRequest(schemas.ChatCompletionRequest, user("hi"), system("A"), system("B"))},
		{"non-string content", chatRequest(schemas.ChatCompletionRequest, system("A"), blocks, user("hi"))},
		{"nil content", chatRequest(schemas.ChatCompletionRequest, system("A"), schemas.ChatMessage{Role: schemas.ChatMessageRoleSystem}, user("hi"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var before []schemas.ChatMessage
			if tt.req != nil && tt.req.ChatRequest != nil {
				before = tt.req.ChatRequest.Input
			}
			got, _, err := PreLLMHook(nil, tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.req {
				t.Fatalf("request was replaced")
			}
			if tt.req != nil && tt.req.ChatRequest != nil && len(tt.req.ChatRequest.Input) != len(before) {
				t.Fatalf("messages changed: %v", roles(tt.req.ChatRequest.Input))
			}
		})
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
