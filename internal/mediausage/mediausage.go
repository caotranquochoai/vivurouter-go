package mediausage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/local/vivurouter-go/internal/store"
)

func AnalyzeJSON(operation, source string, raw []byte) store.MediaMetrics {
	m := store.MediaMetrics{Source: source, Operation: operation, RequestBytes: int64(len(raw))}
	var body map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&body) != nil {
		return m
	}
	switch operation {
	case "image_generation":
		m.ImageCount = positiveInt(body["n"], 1)
		m.ImageSize = bounded(stringValue(body["size"]), 32)
		m.ImageQuality = bounded(stringValue(body["quality"]), 32)
		m.ImageWidth, m.ImageHeight = parseSize(m.ImageSize)
	case "text_to_speech":
		m.TTSCharacters = utf8.RuneCountInString(stringValue(body["input"]))
	}
	return m
}

func AnalyzeMultipart(operation, source, contentType string, raw []byte) store.MediaMetrics {
	m := store.MediaMetrics{Source: source, Operation: operation, RequestBytes: int64(len(raw))}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return m
	}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		data := make([]byte, 0)
		if part.FileName() != "" {
			data, _ = ioReadLimit(part, 32<<20)
			m.InputFileCount++
		}
		if operation == "speech_to_text" && part.FormName() == "file" {
			m.STTDurationMS = wavDuration(data)
		}
		if part.FileName() == "" {
			value, _ := ioReadLimit(part, 1024)
			switch part.FormName() {
			case "n":
				m.ImageCount = positiveInt(string(value), 1)
			case "size":
				m.ImageSize = bounded(string(value), 32)
			case "quality":
				m.ImageQuality = bounded(string(value), 32)
			}
		}
		part.Close()
	}
	if operation == "image_edit" {
		if m.ImageCount == 0 {
			m.ImageCount = 1
		}
		m.ImageWidth, m.ImageHeight = parseSize(m.ImageSize)
	}
	return m
}

func wavDuration(raw []byte) int64 {
	if len(raw) < 44 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return 0
	}
	byteRate := binary.LittleEndian.Uint32(raw[28:32])
	if byteRate == 0 {
		return 0
	}
	for pos := 12; pos+8 <= len(raw); {
		size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
		if string(raw[pos:pos+4]) == "data" {
			return int64(size) * 1000 / int64(byteRate)
		}
		pos += 8 + size
		if pos%2 != 0 {
			pos++
		}
	}
	return 0
}

func parseSize(value string) (int, int) {
	parts := strings.Split(strings.ToLower(value), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 {
		return 0, 0
	}
	return w, h
}
func positiveInt(v any, fallback int) int {
	switch x := v.(type) {
	case json.Number:
		if n, e := strconv.Atoi(string(x)); e == nil && n > 0 && n <= 100 {
			return n
		}
	case string:
		if n, e := strconv.Atoi(x); e == nil && n > 0 && n <= 100 {
			return n
		}
	}
	return fallback
}
func stringValue(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }
func bounded(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}
func ioReadLimit(r interface{ Read([]byte) (int, error) }, limit int64) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(&limitedReader{r: r, n: limit})
	return b.Bytes(), err
}

type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, e := l.r.Read(p)
	l.n -= int64(n)
	return n, e
}
