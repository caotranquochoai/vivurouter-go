package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/auth"
	"github.com/local/vivurouter-go/internal/mediausage"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

const maxMediaParts = 32

type mediaIngress struct {
	model       string
	contentType string
	body        []byte
	metadata    map[string]any
}

type mediaEndpoint struct {
	path      string
	multipart bool
}

func (h *Handler) ImageGenerations(w http.ResponseWriter, r *http.Request) {
	h.handleJSONMedia(w, r, mediaEndpoint{path: "/images/generations"}, []string{"prompt"})
}

func (h *Handler) AudioSpeech(w http.ResponseWriter, r *http.Request) {
	h.handleJSONMedia(w, r, mediaEndpoint{path: "/audio/speech"}, []string{"input", "voice"})
}

func (h *Handler) ImageEdits(w http.ResponseWriter, r *http.Request) {
	h.handleMultipartMedia(w, r, mediaEndpoint{path: "/images/edits", multipart: true}, map[string]bool{"image": true, "mask": true}, []string{"image", "prompt"})
}

func (h *Handler) AudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	h.handleMultipartMedia(w, r, mediaEndpoint{path: "/audio/transcriptions", multipart: true}, map[string]bool{"file": true}, []string{"file"})
}

func (h *Handler) handleJSONMedia(w http.ResponseWriter, r *http.Request, endpoint mediaEndpoint, required []string) {
	if methodAllowed(w, r, http.MethodPost) {
		return
	}
	started := time.Now()
	body, err := readJSONBody(r)
	if err != nil {
		writeMediaReadError(w, err)
		return
	}
	model := getString(body, "model")
	if model == "" {
		writeErrorCode(w, http.StatusBadRequest, "missing model", "invalid_request")
		return
	}
	for _, field := range required {
		if getString(body, field) == "" {
			writeErrorCode(w, http.StatusBadRequest, "missing "+field, "invalid_request")
			return
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid JSON body", "invalid_request")
		return
	}
	metadata := map[string]any{"operation": strings.TrimPrefix(endpoint.path, "/"), "request_bytes": len(raw)}
	h.executeMediaOnce(w, r, started, endpoint, mediaIngress{model: model, contentType: "application/json", body: raw, metadata: metadata})
}

func (h *Handler) handleMultipartMedia(w http.ResponseWriter, r *http.Request, endpoint mediaEndpoint, fileFields map[string]bool, required []string) {
	if methodAllowed(w, r, http.MethodPost) {
		return
	}
	started := time.Now()
	ingress, fields, fileCounts, err := readMediaMultipart(r, fileFields)
	if err != nil {
		writeMediaReadError(w, err)
		return
	}
	model := strings.TrimSpace(firstValue(fields["model"]))
	if model == "" {
		writeErrorCode(w, http.StatusBadRequest, "missing model", "invalid_request")
		return
	}
	for _, field := range required {
		if fileFields[field] {
			if fileCounts[field] == 0 {
				writeErrorCode(w, http.StatusBadRequest, "missing "+field, "invalid_request")
				return
			}
		} else if strings.TrimSpace(firstValue(fields[field])) == "" {
			writeErrorCode(w, http.StatusBadRequest, "missing "+field, "invalid_request")
			return
		}
	}
	ingress.model = model
	ingress.metadata["operation"] = strings.TrimPrefix(endpoint.path, "/")
	ingress.metadata["file_count"] = sumCounts(fileCounts)
	h.executeMediaOnce(w, r, started, endpoint, ingress)
}

func (h *Handler) executeMediaOnce(w http.ResponseWriter, r *http.Request, started time.Time, endpoint mediaEndpoint, ingress mediaIngress) {
	settings, providers, apiKey, ok := h.loadGatewayState(w, r)
	if !ok {
		return
	}
	if !auth.KeyAllowsModel(apiKey, ingress.model) {
		writeError(w, http.StatusForbidden, "API key is not allowed to use model "+ingress.model)
		return
	}
	if !auth.KeyWithinQuota(apiKey) {
		writeError(w, http.StatusTooManyRequests, "API key quota exhausted")
		return
	}
	release, admitted := h.admitAPIKeyRequest(w, apiKey)
	if !admitted {
		return
	}
	defer release()

	candidates := resolveCandidates(ingress.model, settings, providers)
	filtered := candidates[:0]
	for _, cand := range candidates {
		if cand.Provider.Type == store.ProviderOpenAICompatible {
			filtered = append(filtered, cand)
		}
	}
	filtered = h.expandAccountCandidates(r.Context(), filtered, time.Now().UTC())
	var cand resolvedModel
	found := false
	for _, candidate := range filtered {
		if h.observe.Cooldowns.Available(candidate.Provider.ID, time.Now().UTC()) {
			cand, found = candidate, true
			break
		}
	}
	if !found {
		writeErrorCode(w, http.StatusServiceUnavailable, "no OpenAI-compatible provider supports this media request", "no_provider_available")
		return
	}

	media := mediausage.AnalyzeJSON(mediaOperation(endpoint.path), "public_api", ingress.body)
	if endpoint.multipart {
		media = mediausage.AnalyzeMultipart(mediaOperation(endpoint.path), "public_api", ingress.contentType, ingress.body)
	}
	body := ingress.body
	if endpoint.multipart {
		var err error
		body, ingress.contentType, err = replaceMultipartModel(body, ingress.contentType, cand.Model)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, "invalid multipart body", "invalid_request")
			return
		}
	} else {
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err == nil {
			decoded["model"] = cand.Model
			body, _ = json.Marshal(decoded)
		}
	}

	requestCtx, cancel := withGatewayDeadline(r.Context(), h.requestTimeout)
	defer cancel()
	providerStarted := time.Now()
	result, err := h.executors.OpenAI.ExecuteMedia(requestCtx, cand.Provider, provider.MediaRequest{Path: endpoint.path, ContentType: ingress.contentType, Body: bytes.NewReader(body)})
	if err != nil {
		decision := classifyAttemptError(requestCtx, err)
		h.observe.Metrics.RecordUpstreamFailure()
		h.recordAccountOutcome(cand, decision, false, time.Now().UTC())
		h.logMediaRequest(settings, endpoint.path, cand, started, "FAILED", string(decision.Class), media, apiKey, time.Since(providerStarted).Milliseconds())
		if r.Context().Err() == nil {
			writeGatewayError(w, failurePublicError(decision))
		}
		return
	}
	if result == nil || result.Response == nil {
		writeErrorCode(w, http.StatusBadGateway, "upstream returned no response", "upstream_error")
		return
	}
	resp := result.Response
	defer resp.Body.Close()
	decision := classifyResponse(resp.StatusCode, resp.Header)
	if decision.Class != "" {
		h.observe.Metrics.RecordUpstreamFailure()
		h.recordAccountOutcome(cand, decision, false, time.Now().UTC())
		h.logMediaRequest(settings, endpoint.path, cand, started, strconv.Itoa(resp.StatusCode), string(decision.Class), media, apiKey, time.Since(providerStarted).Milliseconds())
		passthroughResponse(w, resp)
		return
	}
	counted := &countingResponseWriter{ResponseWriter: w}
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, copyErr := io.Copy(counted, resp.Body)
	media.ResponseBytes = counted.Bytes
	h.recordAccountOutcome(cand, failureDecision{}, true, time.Now().UTC())
	status := strconv.Itoa(resp.StatusCode)
	if copyErr != nil {
		status = "STREAM_ERROR"
	}
	h.logMediaRequest(settings, endpoint.path, cand, started, status, errString(copyErr), media, apiKey, time.Since(providerStarted).Milliseconds())
}

func (h *Handler) logMediaRequest(settings store.Settings, endpoint string, cand resolvedModel, started time.Time, status, errText string, media store.MediaMetrics, apiKey store.APIKeyPolicy, providerDurationMS int64) {
	if settings.ObservabilityEnabled {
		prefix, suffix, masked := maskedAPIKeyParts(apiKey.Key)
		_ = h.store.AddRequestLog(store.RequestLog{Timestamp: time.Now().UTC(), Endpoint: endpoint, ProviderID: cand.Provider.ID, ProviderAccountID: cand.AccountID, Model: cand.Model, Status: status, DurationMS: time.Since(started).Milliseconds(), ProviderDurationMS: providerDurationMS, CostUSD: media.CostUnits * media.UnitPriceUSD, APIKeyID: apiKey.ID, APIKeyMasked: masked, APIKeyPrefix: prefix, APIKeySuffix: suffix, Error: errText, Media: media})
	}
	if apiKey.ID != "" && apiKey.ID != "local" {
		_ = h.store.RecordAPIKeyUsage(apiKey.ID, store.APIKeyUsageDelta{Requests: 1, CostUSD: media.CostUnits * media.UnitPriceUSD})
	}
}

func mediaOperation(path string) string {
	switch path {
	case "/images/generations":
		return "image_generation"
	case "/images/edits":
		return "image_edit"
	case "/audio/speech":
		return "text_to_speech"
	case "/audio/transcriptions":
		return "speech_to_text"
	}
	return strings.Trim(path, "/")
}

type countingResponseWriter struct {
	http.ResponseWriter
	Bytes int64
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.Bytes += int64(n)
	return n, err
}

func readMediaMultipart(r *http.Request, allowedFileFields map[string]bool) (mediaIngress, map[string][]string, map[string]int, error) {
	defer r.Body.Close()
	if r.ContentLength > maxRequestBytes {
		return mediaIngress{}, nil, nil, ErrBodyTooLarge
	}
	mediaType, params, err := mimeParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return mediaIngress{}, nil, nil, errors.New("multipart/form-data required")
	}
	limited := io.LimitReader(r.Body, maxRequestBytes+1)
	reader := multipart.NewReader(limited, params["boundary"])
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	fields := map[string][]string{}
	fileCounts := map[string]int{}
	parts := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return mediaIngress{}, nil, nil, err
		}
		parts++
		if parts > maxMediaParts {
			return mediaIngress{}, nil, nil, errors.New("too many multipart parts")
		}
		name, filename := part.FormName(), part.FileName()
		if name == "" {
			return mediaIngress{}, nil, nil, errors.New("unnamed multipart part")
		}
		data, err := io.ReadAll(io.LimitReader(part, maxRequestBytes+1))
		part.Close()
		if err != nil || int64(out.Len()+len(data)) > maxRequestBytes {
			return mediaIngress{}, nil, nil, ErrBodyTooLarge
		}
		if filename != "" {
			if !allowedFileFields[name] {
				return mediaIngress{}, nil, nil, fmt.Errorf("unexpected file field %s", name)
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, name, filename))
			if ct := part.Header.Get("Content-Type"); ct != "" {
				header.Set("Content-Type", ct)
			}
			dst, _ := writer.CreatePart(header)
			_, _ = dst.Write(data)
			fileCounts[name]++
		} else {
			fields[name] = append(fields[name], string(data))
			_ = writer.WriteField(name, string(data))
		}
	}
	_ = writer.Close()
	if int64(out.Len()) > maxRequestBytes {
		return mediaIngress{}, nil, nil, ErrBodyTooLarge
	}
	return mediaIngress{contentType: writer.FormDataContentType(), body: out.Bytes(), metadata: map[string]any{"request_bytes": out.Len()}}, fields, fileCounts, nil
}

func replaceMultipartModel(raw []byte, contentType, model string) ([]byte, string, error) {
	mediaType, params, err := mimeParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return nil, "", errors.New("invalid multipart content type")
	}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		data, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			return nil, "", err
		}
		if part.FormName() == "model" && part.FileName() == "" {
			data = []byte(model)
		}
		dst, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, "", err
		}
		_, _ = dst.Write(data)
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

func writeMediaReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBodyTooLarge) {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "request body exceeds configured size limit", "payload_too_large")
		return
	}
	writeErrorCode(w, http.StatusBadRequest, err.Error(), "invalid_request")
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func sumCounts(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

var mimeParseMediaType = mime.ParseMediaType
