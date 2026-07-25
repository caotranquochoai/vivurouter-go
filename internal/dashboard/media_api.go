package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/mediausage"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

const mediaStudioRequestLimit int64 = 128 << 20

var mediaStudioPaths = map[string]string{
	"image":      "/images/generations",
	"image-edit": "/images/edits",
	"tts":        "/audio/speech",
	"stt":        "/audio/transcriptions",
}

// MediaRunAPI executes one explicitly selected OpenAI-compatible media request
// with server-side credentials. Credentials are never returned to the browser.
func (h *Handlers) MediaRunAPI(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	kind := strings.TrimPrefix(r.URL.Path, "/api/media/run/")
	path, ok := mediaStudioPaths[kind]
	if !ok {
		writeError(w, http.StatusNotFound, "unsupported media operation")
		return
	}
	providerID := strings.TrimSpace(r.Header.Get("X-VivuRouter-Provider"))
	if providerID == "" {
		writeError(w, http.StatusBadRequest, "missing provider")
		return
	}
	selected, found, err := h.store.GetProvider(providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "provider configuration is unavailable")
		return
	}
	if !found || !selected.Enabled || (selected.Type != store.ProviderOpenAICompatible && selected.Type != store.ProviderCodex) {
		writeError(w, http.StatusBadRequest, "selected provider is not enabled for this media operation")
		return
	}
	selected, accountID := h.mediaStudioProviderAccount(selected)

	body, contentType, err := readMediaStudioBody(r, kind)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMediaStudioTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, err.Error())
		return
	}
	media := mediausage.AnalyzeJSON(mediaStudioOperation(kind), "media_studio", body)
	if kind == "image-edit" || kind == "stt" {
		media = mediausage.AnalyzeMultipart(mediaStudioOperation(kind), "media_studio", contentType, body)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	var result *provider.ExecuteResult
	if selected.Type == store.ProviderCodex {
		if kind != "image" && kind != "image-edit" {
			writeError(w, http.StatusBadRequest, "Codex OAuth supports image operations only")
			return
		}
		model, prompt, references, options, parseErr := parseCodexStudioImage(body, contentType, kind)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, parseErr.Error())
			return
		}
		result, err = h.executors.Codex.ExecuteImage(ctx, selected, accountID, model, prompt, references, options)
	} else {
		result, err = h.executors.OpenAI.ExecuteMedia(ctx, selected, provider.MediaRequest{Path: path, ContentType: contentType, Body: bytes.NewReader(body)})
	}
	if err != nil {
		h.logMediaStudio(selected, accountID, mediaModel(body, contentType), kind, "FAILED", media, started, "provider request failed")
		if r.Context().Err() == nil {
			writeError(w, http.StatusBadGateway, "media provider request failed")
		}
		return
	}
	if result == nil || result.Response == nil {
		writeError(w, http.StatusBadGateway, "media provider returned no response")
		return
	}
	defer result.Response.Body.Close()
	copyMediaStudioHeaders(w.Header(), result.Response.Header)
	w.WriteHeader(result.Response.StatusCode)
	counted := &mediaStudioCounter{Writer: w}
	_, _ = io.Copy(counted, io.LimitReader(result.Response.Body, mediaStudioRequestLimit+1))
	media.ResponseBytes = counted.Bytes
	h.logMediaStudio(selected, accountID, mediaModel(body, contentType), kind, strconv.Itoa(result.Response.StatusCode), media, started, "")
}

func (h *Handlers) mediaStudioProviderAccount(p store.Provider) (store.Provider, string) {
	accounts, err := h.store.ListProviderAccounts(p.ID)
	if err != nil {
		return p, ""
	}
	now := time.Now().UTC()
	for _, account := range accounts {
		if !account.Enabled || (!account.CooldownUntil.IsZero() && now.Before(account.CooldownUntil)) {
			continue
		}
		effective := p
		effective.APIKey = account.APIKey
		effective.AccessToken = account.AccessToken
		effective.RefreshToken = account.RefreshToken
		effective.Keys = nil
		if account.ProxyID != "" || account.ProxyURL != "" {
			effective.ProxyID = account.ProxyID
			effective.ProxyURL = account.ProxyURL
		}
		return effective, account.ID
	}
	return p, ""
}

var errMediaStudioTooLarge = errors.New("media request exceeds size limit")

func readMediaStudioBody(r *http.Request, kind string) ([]byte, string, error) {
	defer r.Body.Close()
	if r.ContentLength > mediaStudioRequestLimit {
		return nil, "", errMediaStudioTooLarge
	}
	limited := io.LimitReader(r.Body, mediaStudioRequestLimit+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", errors.New("invalid media request")
	}
	if int64(len(raw)) > mediaStudioRequestLimit {
		return nil, "", errMediaStudioTooLarge
	}
	contentType := r.Header.Get("Content-Type")
	if kind == "image" || kind == "tts" {
		var body map[string]any
		if !strings.HasPrefix(strings.ToLower(contentType), "application/json") || json.Unmarshal(raw, &body) != nil {
			return nil, "", errors.New("JSON request required")
		}
		if strings.TrimSpace(stringValue(body["model"])) == "" {
			return nil, "", errors.New("missing model")
		}
		return raw, "application/json", nil
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, "", errors.New("multipart/form-data request required")
	}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	parts := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", errors.New("invalid multipart request")
		}
		parts++
		part.Close()
		if parts > 32 {
			return nil, "", errors.New("too many multipart parts")
		}
	}
	return raw, contentType, nil
}

func parseCodexStudioImage(raw []byte, contentType, kind string) (string, string, []string, map[string]string, error) {
	options := map[string]string{}
	if kind == "image" {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			return "", "", nil, nil, errors.New("invalid image request")
		}
		for _, key := range []string{"size", "quality", "background", "output_format"} {
			options[key] = stringValue(body[key])
		}
		return stringValue(body["model"]), stringValue(body["prompt"]), nil, options, nil
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", "", nil, nil, errors.New("invalid image edit request")
	}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	model, prompt := "", ""
	references := []string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", nil, nil, errors.New("invalid image edit request")
		}
		data, readErr := io.ReadAll(io.LimitReader(part, mediaStudioRequestLimit+1))
		if readErr != nil {
			part.Close()
			return "", "", nil, nil, errors.New("invalid image edit request")
		}
		name, filename := part.FormName(), part.FileName()
		content := part.Header.Get("Content-Type")
		part.Close()
		if filename != "" && (name == "image" || name == "mask") {
			if content == "" {
				content = "image/png"
			}
			references = append(references, "data:"+content+";base64,"+base64.StdEncoding.EncodeToString(data))
			continue
		}
		switch name {
		case "model":
			model = string(data)
		case "prompt":
			prompt = string(data)
		case "size", "quality", "background", "output_format":
			options[name] = string(data)
		}
	}
	if strings.TrimSpace(model) == "" || strings.TrimSpace(prompt) == "" || len(references) == 0 {
		return "", "", nil, nil, errors.New("model, prompt, and image are required")
	}
	return model, prompt, references, options, nil
}

func (h *Handlers) logMediaStudio(p store.Provider, accountID, model, kind, status string, media store.MediaMetrics, started time.Time, errText string) {
	settings, err := h.store.GetSettings()
	if err != nil || !settings.ObservabilityEnabled {
		return
	}
	_ = h.store.AddRequestLog(store.RequestLog{Timestamp: time.Now().UTC(), Endpoint: "/api/media/run/" + kind, ProviderID: p.ID, ProviderAccountID: accountID, Model: model, Status: status, DurationMS: time.Since(started).Milliseconds(), Error: errText, Media: media, CostUSD: media.CostUnits * media.UnitPriceUSD})
}

func mediaStudioOperation(kind string) string {
	switch kind {
	case "image":
		return "image_generation"
	case "image-edit":
		return "image_edit"
	case "tts":
		return "text_to_speech"
	case "stt":
		return "speech_to_text"
	}
	return kind
}
func mediaModel(raw []byte, contentType string) string {
	if strings.HasPrefix(contentType, "application/json") {
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		return stringValue(body["model"])
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	for {
		part, e := reader.NextPart()
		if e != nil {
			return ""
		}
		if part.FormName() == "model" {
			data, _ := io.ReadAll(io.LimitReader(part, 512))
			part.Close()
			return strings.TrimSpace(string(data))
		}
		part.Close()
	}
}

type mediaStudioCounter struct {
	Writer io.Writer
	Bytes  int64
}

func (w *mediaStudioCounter) Write(p []byte) (int, error) {
	n, e := w.Writer.Write(p)
	w.Bytes += int64(n)
	return n, e
}

func copyMediaStudioHeaders(dst, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "connection", "content-length", "content-encoding", "keep-alive", "proxy-authenticate", "proxy-authorization", "set-cookie", "trailer", "transfer-encoding", "upgrade":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
