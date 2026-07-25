package provider

import (
	"strings"
	"testing"
)

func TestReadCodexImageSSE(t *testing.T) {
	stream := "event: response.created\ndata: {}\n\n" +
		"event: response.output_item.done\ndata: {\"item\":{\"type\":\"image_generation_call\",\"result\":\"aW1hZ2U=\"}}\n\n"
	got, err := readCodexImageSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if got != "aW1hZ2U=" {
		t.Fatalf("image = %q", got)
	}
}

func TestReadCodexImageSSERequiresImage(t *testing.T) {
	if _, err := readCodexImageSSE(strings.NewReader("event: response.completed\ndata: {}\n\n")); err == nil {
		t.Fatal("expected missing image error")
	}
}
