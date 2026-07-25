package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestReadJSONBodyRejectsOversizePayload(t *testing.T) {
	previous := maxRequestBytes
	t.Cleanup(func() { maxRequestBytes = previous })
	SetRequestBodyLimit(16)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"this-is-too-large"}`))
	_, err := readJSONBody(req)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("readJSONBody() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestReadNonStreamResponseRejectsOversizePayload(t *testing.T) {
	previous := maxNonStreamResponseBytes
	t.Cleanup(func() { maxNonStreamResponseBytes = previous })
	SetNonStreamResponseLimit(4)
	_, err := readNonStreamResponse(strings.NewReader("12345"))
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("readNonStreamResponse() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestDebugPayloadHonorsRuntimeCap(t *testing.T) {
	previous := maxDebugPayloadBytes
	t.Cleanup(func() { maxDebugPayloadBytes = previous })
	SetDebugPayloadLimit(8)
	payload := buildDebugPayload(store.Settings{SaveRawPrompt: true, MaxDebugPayloadBytes: 1024}, map[string]any{"prompt": "this text is longer than the cap"}, "")
	if payload == nil || len(payload.RawPrompt) > 32 {
		t.Fatalf("debug payload did not honor cap: %+v", payload)
	}
}

func TestWriteGatewayErrorRedactsDiagnosticCause(t *testing.T) {
	rec := httptest.NewRecorder()
	writeGatewayError(rec, errors.New("dial https://secret-upstream.example: token leaked"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if got := rec.Body.String(); strings.Contains(got, "secret-upstream") || strings.Contains(got, "token leaked") {
		t.Fatalf("raw diagnostic leaked to client: %s", got)
	}
	if !strings.Contains(rec.Body.String(), `"code":"upstream_error"`) {
		t.Fatalf("stable error code missing: %s", rec.Body.String())
	}
}
