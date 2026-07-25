package mediausage

import (
	"bytes"
	"encoding/binary"
	"mime/multipart"
	"testing"
)

func TestAnalyzeJSONMedia(t *testing.T) {
	image := AnalyzeJSON("image_generation", "public_api", []byte(`{"prompt":"secret","n":2,"size":"1024x1536","quality":"high"}`))
	if image.ImageCount != 2 || image.ImageWidth != 1024 || image.ImageHeight != 1536 || image.ImageQuality != "high" {
		t.Fatalf("image metrics = %+v", image)
	}
	tts := AnalyzeJSON("text_to_speech", "public_api", []byte(`{"input":"Xin chào 👋"}`))
	if tts.TTSCharacters != 10 {
		t.Fatalf("characters = %d", tts.TTSCharacters)
	}
}

func TestAnalyzeWAVDuration(t *testing.T) {
	wav := make([]byte, 44+16000)
	copy(wav[0:4], "RIFF")
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint32(wav[28:32], 16000)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], 16000)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "secret.wav")
	_, _ = part.Write(wav)
	_ = writer.WriteField("model", "whisper-1")
	_ = writer.Close()
	m := AnalyzeMultipart("speech_to_text", "media_studio", writer.FormDataContentType(), body.Bytes())
	if m.STTDurationMS != 1000 || m.InputFileCount != 1 {
		t.Fatalf("metrics = %+v", m)
	}
}
