package gateway

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadMediaMultipartPreservesFilesAndFields(t *testing.T) {
	var raw bytes.Buffer
	writer := multipart.NewWriter(&raw)
	_ = writer.WriteField("model", "whisper-1")
	_ = writer.WriteField("language", "vi")
	file, _ := writer.CreateFormFile("file", "sample.wav")
	_, _ = file.Write([]byte("RIFFtest"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(raw.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ingress, fields, files, err := readMediaMultipart(req, map[string]bool{"file": true})
	if err != nil {
		t.Fatal(err)
	}
	if firstValue(fields["model"]) != "whisper-1" || firstValue(fields["language"]) != "vi" || files["file"] != 1 {
		t.Fatalf("unexpected fields=%v files=%v", fields, files)
	}
	mediaType, params, err := mimeParseMediaType(ingress.contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q, err=%v", ingress.contentType, err)
	}
	reader := multipart.NewReader(bytes.NewReader(ingress.body), params["boundary"])
	seenFile := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() == "file" {
			seenFile = true
			data, _ := io.ReadAll(part)
			if string(data) != "RIFFtest" {
				t.Fatalf("file data = %q", data)
			}
		}
	}
	if !seenFile {
		t.Fatal("file part not preserved")
	}
}

func TestReadMediaMultipartRejectsOversizeAndUnexpectedFile(t *testing.T) {
	previous := maxRequestBytes
	t.Cleanup(func() { maxRequestBytes = previous })
	SetRequestBodyLimit(32)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(strings.Repeat("x", 64)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	if _, _, _, err := readMediaMultipart(req, map[string]bool{"image": true}); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
}

func TestReplaceMultipartModel(t *testing.T) {
	var raw bytes.Buffer
	writer := multipart.NewWriter(&raw)
	_ = writer.WriteField("model", "provider/model")
	_ = writer.WriteField("prompt", "hello")
	_ = writer.Close()

	body, contentType, err := replaceMultipartModel(raw.Bytes(), writer.FormDataContentType(), "model")
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mimeParseMediaType(contentType)
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	values := map[string]string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		data, _ := io.ReadAll(part)
		values[part.FormName()] = string(data)
	}
	if values["model"] != "model" || values["prompt"] != "hello" {
		t.Fatalf("values = %v", values)
	}
}
