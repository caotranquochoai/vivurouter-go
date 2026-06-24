package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

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
	var body map[string]any
	decoder := json.NewDecoder(io.LimitReader(r.Body, 128*1024*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return nil, err
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
