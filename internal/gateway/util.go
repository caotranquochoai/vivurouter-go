package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrBodyTooLarge = errors.New("request body exceeds configured limit")

var maxRequestBytes int64 = 128 * 1024 * 1024
var maxNonStreamResponseBytes int64 = 32 * 1024 * 1024

// SetRequestBodyLimit configures the maximum gateway JSON ingress size.
func SetRequestBodyLimit(limit int64) {
	if limit > 0 {
		maxRequestBytes = limit
	}
}

// SetNonStreamResponseLimit configures the maximum transformed upstream JSON size.
func SetNonStreamResponseLimit(limit int64) {
	if limit > 0 {
		maxNonStreamResponseBytes = limit
	}
}

func readNonStreamResponse(body io.Reader) ([]byte, error) {
	limited := io.LimitReader(body, maxNonStreamResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxNonStreamResponseBytes {
		return nil, ErrBodyTooLarge
	}
	return raw, nil
}

func nowUnix() int64 {
	return time.Now().Unix()
}

func nowUnixMillis() int64 {
	return time.Now().UnixMilli()
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func bodyStreamRequested(body map[string]any) bool {
	value, ok := body["stream"].(bool)
	return ok && value
}

func readJSONBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	if r.ContentLength > maxRequestBytes {
		return nil, ErrBodyTooLarge
	}
	limited := http.MaxBytesReader(nil, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(limited)
	decoder.UseNumber()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, ErrBodyTooLarge
		}
		return nil, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("request body must contain one JSON value")
	}
	return body, nil
}

func getString(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return strings.TrimSpace(value)
}

func setModel(body map[string]any, model string) map[string]any {
	out := cloneMap(body)
	out["model"] = model
	return out
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func passthroughResponse(w http.ResponseWriter, resp *http.Response) {
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func methodAllowed(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	for _, method := range methods {
		if r.Method == method {
			return false
		}
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return true
}
