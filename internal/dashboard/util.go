package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isSuccessStatus(status string) bool {
	status = strings.TrimSpace(status)
	if len(status) == 3 && strings.HasPrefix(status, "2") {
		return true
	}
	return strings.EqualFold(status, "OK") || strings.EqualFold(status, "SUCCESS")
}

func splitModels(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' })
	models := []string{}
	seen := map[string]bool{}
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (h *Handlers) databasePath() (string, error) {
	if h.cfg.StoreBackend == "sqlite" {
		return filepath.Join(h.cfg.DataDir, "sample.sqlite"), nil
	}
	return filepath.Join(h.cfg.DataDir, "sample-db.json"), nil
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeUploadedFile(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(r, 128<<20))
	return err
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": message}})
}
