package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHistory(t *testing.T) {
	s := &Service{}
	req := Request{Messages: []Message{
		{Role: "assistant", Content: "dropped: transcript must start with the user"},
		{Role: "user", Content: "brushed nickel kitchen faucet"},
		{Role: "assistant", Content: "Two options.", Shown: []Shown{{30366, "Empressa"}, {37505, "Empressa Bridge"}}},
		{Role: "user", Content: "the second one?"},
		{Role: "user", Content: "in chrome"},
	}}
	msgs, err := s.history(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want 3 turns (user, assistant, merged user), got %d", len(msgs))
	}
	if msgs[0].OfUser == nil || msgs[1].OfAssistant == nil || msgs[2].OfUser == nil {
		t.Fatal("roles out of order")
	}
	b, _ := json.Marshal(msgs[1])
	if !strings.Contains(string(b), "30366: Empressa") || !strings.Contains(string(b), "[products shown") {
		t.Errorf("shown products not carried: %s", b)
	}
	if len(msgs[2].OfUser.Content) != 2 {
		t.Errorf("consecutive user turns should merge into one message, got %d blocks", len(msgs[2].OfUser.Content))
	}
	// an attached picture goes on the last user message, before the text
	req.Image, req.ImageType = "AAAA", "image/jpeg"
	msgs, err = s.history(req)
	if err != nil {
		t.Fatal(err)
	}
	last := msgs[len(msgs)-1].OfUser
	if last.Content[len(last.Content)-2].OfImage == nil {
		t.Error("image block missing")
	}
	if _, err := s.history(Request{ImageType: "image/tiff", Image: "x", Messages: []Message{{Role: "user", Content: "hi"}}}); err == nil {
		t.Error("tiff should be rejected")
	}
	if _, err := s.history(Request{Messages: []Message{{Role: "assistant", Content: "only me"}}}); err == nil {
		t.Error("assistant-only transcript should fail")
	}
}

func TestToolsAreStrict(t *testing.T) {
	s := &Service{}
	for _, tl := range s.tools(true) {
		b, err := json.Marshal(tl)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if m["strict"] != true {
			t.Errorf("%v not strict", m["name"])
		}
		schema := m["input_schema"].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Errorf("%v allows extra properties", m["name"])
		}
		props := schema["properties"].(map[string]any)
		req, _ := schema["required"].([]any)
		if len(req) != len(props) {
			t.Errorf("%v: strict schemas must list every property as required (%d props, %d required)", m["name"], len(props), len(req))
		}
	}
}
