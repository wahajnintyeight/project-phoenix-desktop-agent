package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiToolCallSignatureRoundTrip(t *testing.T) {
	var p openaiParser
	p.Parse([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"navigate","arguments":"{\"url\":"},"extra_content":{"google":{"thought_signature":"opaque-signature"}}}]}}]}`))
	p.Parse([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"https://example.com\"}"}}]}}]}`))
	calls := p.toolCalls()
	if len(calls) != 1 || calls[0].Function.Arguments != `{"url":"https://example.com"}` {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	body, err := json.Marshal(chatMessage{Role: "assistant", ToolCalls: calls})
	if err != nil || !strings.Contains(string(body), `"thought_signature":"opaque-signature"`) {
		t.Fatalf("signature missing from replay: %s (%v)", body, err)
	}
}
