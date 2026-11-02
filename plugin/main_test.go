package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/tidwall/gjson"
)

func TestHTTPTransportPreHook(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantBody   string
		wantMerged bool
	}{
		{
			name:   "merges only leading system messages",
			method: "POST",
			path:   "/v1/chat/completions",
			body: `{
				"model":"qwen3.8-27b",
				"messages":[
					{"role":"system","content":"First instruction","name":"primary"},
					{"role":"system","content":"Second instruction"},
					{"role":"user","content":"Hello"},
					{"role":"system","content":"Leave this message in place"}
				]
			}`,
			wantBody: `{
				"model":"qwen3.8-27b",
				"messages":[
					{"role":"system","content":"First instruction\n\nSecond instruction","name":"primary"},
					{"role":"user","content":"Hello"},
					{"role":"system","content":"Leave this message in place"}
				]
			}`,
			wantMerged: true,
		},
		{
			name:       "leaves one leading system message unchanged",
			method:     "POST",
			path:       "/v1/chat/completions",
			body:       `{"messages":[{"role":"system","content":"Only"},{"role":"user","content":"Hello"}]}`,
			wantMerged: false,
		},
		{
			name:       "leaves non-leading system messages unchanged",
			method:     "POST",
			path:       "/v1/chat/completions",
			body:       `{"messages":[{"role":"user","content":"Hello"},{"role":"system","content":"First"},{"role":"system","content":"Second"}]}`,
			wantMerged: false,
		},
		{
			name:       "leaves multimodal system content unchanged",
			method:     "POST",
			path:       "/v1/chat/completions",
			body:       `{"messages":[{"role":"system","content":[{"type":"text","text":"First"}]},{"role":"system","content":"Second"},{"role":"user","content":"Hello"}]}`,
			wantMerged: false,
		},
		{
			name:       "leaves non chat completion requests unchanged",
			method:     "POST",
			path:       "/v1/responses",
			body:       `{"messages":[{"role":"system","content":"First"},{"role":"system","content":"Second"}]}`,
			wantMerged: false,
		},
		{
			name:       "leaves malformed JSON unchanged",
			method:     "POST",
			path:       "/v1/chat/completions",
			body:       `{"messages":[`,
			wantMerged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &schemas.HTTPRequest{
				Method: tt.method,
				Path:   tt.path,
				Body:   []byte(tt.body),
			}

			resp, err := HTTPTransportPreHook(newBifrostContext(), req)
			if err != nil {
				t.Fatalf("HTTPTransportPreHook() error = %v", err)
			}
			if resp != nil {
				t.Fatal("HTTPTransportPreHook() returned an unexpected response")
			}

			if !tt.wantMerged {
				if got := string(req.Body); got != tt.body {
					t.Fatalf("request body changed unexpectedly:\ngot:  %s\nwant: %s", got, tt.body)
				}
				return
			}

			assertJSONEqual(t, tt.wantBody, string(req.Body))
		})
	}
}

func TestHTTPTransportPreHookSupportsOpenAICompatibleChatCompletionPaths(t *testing.T) {
	req := &schemas.HTTPRequest{
		Method: "POST",
		Path:   "/openai/v1/chat/completions",
		Body:   []byte(`{"messages":[{"role":"system","content":"First"},{"role":"system","content":"Second"},{"role":"user","content":"Hello"}]}`),
	}

	if _, err := HTTPTransportPreHook(newBifrostContext(), req); err != nil {
		t.Fatalf("HTTPTransportPreHook() error = %v", err)
	}

	messages := gjson.GetBytes(req.Body, "messages").Array()
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if got := messages[0].Get("content").String(); got != "First\n\nSecond" {
		t.Fatalf("merged content = %q, want %q", got, "First\n\nSecond")
	}
}

func TestHTTPTransportPreHookMergesEmptyStringContent(t *testing.T) {
	req := &schemas.HTTPRequest{
		Method: "POST",
		Path:   "/v1/chat/completions",
		Body:   []byte(`{"messages":[{"role":"system","content":""},{"role":"system","content":"Second"},{"role":"user","content":"Hello"}]}`),
	}

	if _, err := HTTPTransportPreHook(newBifrostContext(), req); err != nil {
		t.Fatalf("HTTPTransportPreHook() error = %v", err)
	}

	messages := gjson.GetBytes(req.Body, "messages").Array()
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if got := messages[0].Get("content").String(); got != "\n\nSecond" {
		t.Fatalf("merged content = %q, want %q", got, "\n\nSecond")
	}
}

func newBifrostContext() *schemas.BifrostContext {
	return schemas.NewBifrostContext(context.Background(), time.Time{})
}

func assertJSONEqual(t *testing.T, want, got string) {
	t.Helper()

	var wantJSON any
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatalf("invalid expected JSON: %v", err)
	}
	var gotJSON any
	if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
		t.Fatalf("invalid actual JSON: %v", err)
	}
	if !reflect.DeepEqual(wantJSON, gotJSON) {
		t.Fatalf("JSON differs:\ngot:  %s\nwant: %s", got, want)
	}
}
