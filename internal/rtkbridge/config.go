package rtkbridge

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

type Config struct {
	Enabled   bool
	Detection Detection
	Runner    Runner
	Message   string
}

func ResolveConfig(settings store.Settings) Config {
	return ResolveConfigForSettings(settings, filepath.Dir(os.Args[0]), runtime.GOOS)
}

func ResolveConfigForSettings(settings store.Settings, appDir string, goos string) Config {
	if !settings.RTKEnabled {
		return Config{Enabled: false, Detection: DetectFromPathSource("", "", "settings", goos), Message: "rtk disabled in settings"}
	}
	detected := DetectFromPathSource(appDir, strings.TrimSpace(settings.RTKPath), "settings", goos)
	cfg := Config{Enabled: true, Detection: detected}
	if !detected.Found {
		cfg.Message = detected.Message
		return cfg
	}
	if !detected.CanRunNow {
		cfg.Message = detected.Message
		return cfg
	}
	cfg.Runner = NewRunner(detected.Path)
	cfg.Runner.Timeout = 5 * time.Second
	cfg.Runner.TempDir = filepath.Join(appDir, "data", "tmp", "rtk")
	cfg.Message = "rtk available"
	return cfg
}
