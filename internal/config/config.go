package config

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config contains runtime settings for the Go sample application.
type Config struct {
	Host            string
	Port            string
	DataDir         string
	AssetsDir       string
	StoreBackend    string
	RequireAPIKey   bool
	LocalAPIKey     string
	Debug           bool
	ShutdownTimeout time.Duration
}

// Load reads configuration from environment variables and applies safe defaults.
func Load() Config {
	cfg := Config{
		Host:            envOr("HOSTNAME", "127.0.0.1"),
		Port:            envOr("PORT", "20129"),
		DataDir:         envOr("DATA_DIR", filepath.Join(".", "data")),
		AssetsDir:       envOr("ASSETS_DIR", ""),
		StoreBackend:    envOr("STORE_BACKEND", "file"),
		RequireAPIKey:   envBool("REQUIRE_API_KEY", false),
		LocalAPIKey:     os.Getenv("LOCAL_API_KEY"),
		Debug:           envBool("DEBUG", false),
		ShutdownTimeout: time.Duration(envInt("SHUTDOWN_TIMEOUT", 15)) * time.Second,
	}

	if cfg.AssetsDir == "" {
		cfg.AssetsDir = resolveAssetsDir()
	}
	return cfg
}

func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

func resolveAssetsDir() string {
	candidates := []string{
		"web",
		filepath.Join("vivurouter-go", "web"),
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return "web"
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
