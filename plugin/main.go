// Package main builds a native Bifrost plugin that merges leading system messages.
package main

import (
	"encoding/json"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	pluginName                = "merge-leading-system-messages"
	chatCompletionsPathSuffix = "/chat/completions"
)

// Init is called by Bifrost when the plugin is loaded.
func Init(_ any) error {
	return nil
}

// GetName returns the plugin's stable identifier.
func GetName() string {
	return pluginName
}

// Cleanup is called by Bifrost during shutdown.
func Cleanup() error {
	return nil
}

// HTTPTransportPreHook merges consecutive string-content system messages only when
// they begin a Chat Completions conversation. It deliberately leaves malformed,
// non-chat, and multimodal requests unchanged.
func HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	if req == nil || req.Method != "POST" || !strings.HasSuffix(req.Path, chatCompletionsPathSuffix) || !json.Valid(req.Body) {
		return nil, nil
	}

	messages := gjson.GetBytes(req.Body, "messages")
	if !messages.IsArray() {
		return nil, nil
	}

	leadingSystemMessages := 0
	for _, message := range messages.Array() {
		if message.Get("role").Type != gjson.String || message.Get("role").String() != "system" {
			break
		}
		leadingSystemMessages++
	}
	if leadingSystemMessages < 2 {
		return nil, nil
	}

	messageArray := messages.Array()
	contents := make([]string, 0, leadingSystemMessages)
	for _, message := range messageArray[:leadingSystemMessages] {
		content := message.Get("content")
		if content.Type != gjson.String {
			return nil, nil
		}
		contents = append(contents, content.String())
	}

	mergedContent, err := json.Marshal(strings.Join(contents, "\n\n"))
	if err != nil {
		return nil, nil
	}
	mergedFirstMessage, err := sjson.SetRawBytes([]byte(messageArray[0].Raw), "content", mergedContent)
	if err != nil {
		return nil, nil
	}

	mergedMessages := make([][]byte, 0, len(messageArray)-leadingSystemMessages+1)
	mergedMessages = append(mergedMessages, mergedFirstMessage)
	for _, message := range messageArray[leadingSystemMessages:] {
		mergedMessages = append(mergedMessages, []byte(message.Raw))
	}

	updatedBody, err := sjson.SetRawBytes(req.Body, "messages", joinJSONArray(mergedMessages))
	if err != nil {
		return nil, nil
	}
	req.Body = updatedBody

	if ctx != nil {
		ctx.Log(schemas.LogLevelInfo, "merged leading system messages into one")
	}
	return nil, nil
}

func joinJSONArray(messages [][]byte) []byte {
	length := 2 // '[' and ']'
	for _, message := range messages {
		length += len(message)
	}
	length += len(messages) - 1 // commas

	result := make([]byte, 0, length)
	result = append(result, '[')
	for i, message := range messages {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, message...)
	}
	result = append(result, ']')
	return result
}
