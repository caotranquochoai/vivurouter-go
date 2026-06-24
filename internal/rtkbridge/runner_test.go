package rtkbridge

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/store"
	"github.com/local/vivurouter-go/internal/tokenopt"
)

func TestRunnerCompactUsesTempFileAndDeletesIt(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRunner(writeFakeRTK(t))
	r.TempDir = tempDir
	r.Timeout = 2 * time.Second
	res, err := r.Compact(context.Background(), CompactModeLog, strings.Repeat("noise\n", 400)+"ERROR preserved\n", tokenopt.Options{MinChars: 1000, MaxChars: 2000, PreserveErrors: true})
	if err != nil {
		t.Fatalf("compact failed: %v", err)
	}
	if !res.Applied || !strings.Contains(res.Text, "rtk helper compacted") {
		t.Fatalf("unexpected compact result: %+v", res)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp input to be deleted, found %d entries", len(entries))
	}
}

func TestRunnerCompactRejectsOversizeInput(t *testing.T) {
	r := NewRunner(writeFakeRTK(t))
	r.MaxInputBytes = 8
	_, err := r.Compact(context.Background(), CompactModeText, "this input is too large", tokenopt.Options{})
	if err == nil {
		t.Fatalf("expected oversize input error")
	}
}

func TestRunnerVersion(t *testing.T) {
	r := NewRunner(writeFakeRTK(t))
	version, err := r.Version(context.Background())
	if err != nil {
		t.Fatalf("version failed: %v", err)
	}
	if version != "rtk helper 0.0.0" {
		t.Fatalf("unexpected version %q", version)
	}
}

func TestResolveConfigUsesSettingsPathSource(t *testing.T) {
	path := writeFakeRTK(t)
	cfg := ResolveConfigForSettings(store.Settings{RTKEnabled: true, RTKPath: path}, filepath.Dir(path), runtime.GOOS)
	if !cfg.Enabled || !cfg.Detection.Found || cfg.Detection.Source != "settings" || !cfg.Detection.CanRunNow {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func writeFakeRTK(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "rtk.cmd")
		content := "@echo off\r\nif \"%1\"==\"--version\" (echo rtk helper 0.0.0& exit /b 0)\r\necho rtk helper compacted\r\necho ERROR preserved\r\n"
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, "rtk")
	content := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'rtk helper 0.0.0'; exit 0; fi\necho 'rtk helper compacted'\necho 'ERROR preserved'\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
