package main

import (
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

// PreLLMHook merges consecutive leading string-content system messages in Chat
// Completions requests, after Bifrost core has finished building the message list.
//
// HTTPTransportPreHook alone is not enough: core adds its own system message after the
// transport hooks have run. When MCP clients with server instructions are configured,
// the tool manager prepends those instructions as a separate system message ahead of the
// caller's (core/mcp/toolmanager.go, addServerInstructionsToRequest), and only then runs
// the LLM plugin pre-hooks. This hook sees that final list, on every attempt including
// fallbacks and each agent-loop turn, so it is where the merge has to happen.
//
// The merged message keeps every field of the first system message, the leading run's
// order is preserved (injected MCP instructions first, the caller's instructions last),
// and the caller's message slice is not modified.
func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	if req == nil || req.ChatRequest == nil {
		return req, nil, nil
	}
	if req.RequestType != schemas.ChatCompletionRequest && req.RequestType != schemas.ChatCompletionStreamRequest {
		return req, nil, nil
	}

	input := req.ChatRequest.Input
	leading := 0
	for leading < len(input) && input[leading].Role == schemas.ChatMessageRoleSystem {
		leading++
	}
	if leading < 2 {
		return req, nil, nil
	}

	contents := make([]string, 0, leading)
	for _, message := range input[:leading] {
		if message.Content == nil || message.Content.ContentStr == nil {
			// Multimodal or block content: leave the request untouched, as the HTTP hook does.
			return req, nil, nil
		}
		contents = append(contents, *message.Content.ContentStr)
	}
	merged := strings.Join(contents, "\n\n")

	first := input[0]
	first.Content = &schemas.ChatMessageContent{ContentStr: &merged}

	messages := make([]schemas.ChatMessage, 0, len(input)-leading+1)
	messages = append(messages, first)
	messages = append(messages, input[leading:]...)

	chatRequest := *req.ChatRequest
	chatRequest.Input = messages
	req.ChatRequest = &chatRequest

	if ctx != nil {
		ctx.Log(schemas.LogLevelInfo, "merged leading system messages into one")
	}
	return req, nil, nil
}
