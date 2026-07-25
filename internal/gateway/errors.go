package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type openAIError struct {
	Error openAIErrorBody `json:"error"`
}

type openAIErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type GatewayError struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *GatewayError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *GatewayError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newGatewayError(status int, code, message string, cause error) *GatewayError {
	return &GatewayError{Status: status, Code: code, Message: message, Cause: cause}
}

func writeGatewayError(w http.ResponseWriter, err error) {
	var gatewayErr *GatewayError
	if errors.As(err, &gatewayErr) {
		writeErrorCode(w, gatewayErr.Status, gatewayErr.Message, gatewayErr.Code)
		return
	}
	if errors.Is(err, ErrBodyTooLarge) {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "request or upstream response exceeds configured size limit", "payload_too_large")
		return
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		writeErrorCode(w, http.StatusBadGateway, "upstream response ended unexpectedly", "upstream_protocol_error")
		return
	}
	writeErrorCode(w, http.StatusBadGateway, "upstream request failed", "upstream_error")
}

func writeErrorCode(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, openAIError{Error: openAIErrorBody{Message: message, Type: "gateway_error", Code: code}})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorCode(w, status, message, "")
}
