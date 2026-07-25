package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DeploymentLocal   = "local"
	DeploymentPrivate = "private"
	DeploymentPublic  = "public"
)

// GatewayLimits bounds request and non-stream response memory use.
type GatewayLimits struct {
	MaxRequestBytes           int64
	MaxNonStreamResponseBytes int64
	MaxDebugPayloadBytes      int64
}

// Config contains runtime settings for the Go sample application.
type Config struct {
	Host                     string
	Port                     string
	DataDir                  string
	AssetsDir                string
	StoreBackend             string
	RequireAPIKey            bool
	LocalAPIKey              string
	Debug                    bool
	ShutdownTimeout          time.Duration
	DeploymentMode           string
	AllowInsecureNonLoopback bool
	GatewayCORSOrigins       []string
	TrustedProxyCIDRs        []string
	GatewayLimits            GatewayLimits
	GatewayRequestTimeout    time.Duration
	GatewayMinFallbackBudget time.Duration
}

// Load reads configuration from environment variables and applies safe defaults.
func Load() Config {
	cfg := Config{
		Host:                     envOr("HOSTNAME", "127.0.0.1"),
		Port:                     envOr("PORT", "20129"),
		DataDir:                  envOr("DATA_DIR", filepath.Join(".", "data")),
		AssetsDir:                envOr("ASSETS_DIR", ""),
		StoreBackend:             envOr("STORE_BACKEND", "file"),
		RequireAPIKey:            envBool("REQUIRE_API_KEY", false),
		LocalAPIKey:              os.Getenv("LOCAL_API_KEY"),
		Debug:                    envBool("DEBUG", false),
		ShutdownTimeout:          time.Duration(envInt("SHUTDOWN_TIMEOUT", 15)) * time.Second,
		DeploymentMode:           strings.ToLower(strings.TrimSpace(os.Getenv("DEPLOYMENT_MODE"))),
		AllowInsecureNonLoopback: envBool("ALLOW_INSECURE_NON_LOOPBACK", false),
		GatewayCORSOrigins:       envCSV("GATEWAY_CORS_ORIGINS"),
		TrustedProxyCIDRs:        envCSV("TRUSTED_PROXY_CIDRS"),
		GatewayRequestTimeout:    time.Duration(envInt("GATEWAY_REQUEST_TIMEOUT", 180)) * time.Second,
		GatewayMinFallbackBudget: time.Duration(envInt("GATEWAY_MIN_FALLBACK_BUDGET", 5)) * time.Second,
		GatewayLimits: GatewayLimits{
			MaxRequestBytes:           int64(envInt("GATEWAY_MAX_REQUEST_BYTES", 128*1024*1024)),
			MaxNonStreamResponseBytes: int64(envInt("GATEWAY_MAX_NON_STREAM_RESPONSE_BYTES", 32*1024*1024)),
			MaxDebugPayloadBytes:      int64(envInt("GATEWAY_MAX_DEBUG_PAYLOAD_BYTES", 4*1024*1024)),
		},
	}

	if cfg.GatewayMinFallbackBudget > cfg.GatewayRequestTimeout {
		cfg.GatewayMinFallbackBudget = cfg.GatewayRequestTimeout
	}
	if cfg.AssetsDir == "" {
		cfg.AssetsDir = resolveAssetsDir()
	}
	return cfg
}

func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

// IsLoopbackBind reports whether the configured listener is local-only.
func (c Config) IsLoopbackBind() bool {
	host := strings.TrimSpace(c.Host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// EffectiveDeploymentMode preserves the historical safe local default while
// requiring an explicit intent for non-loopback listeners.
func (c Config) EffectiveDeploymentMode() string {
	if c.DeploymentMode != "" {
		return c.DeploymentMode
	}
	if c.IsLoopbackBind() {
		return DeploymentLocal
	}
	return ""
}

// ValidateDeploymentMode checks syntax and the explicit-mode requirement. The
// startup package additionally validates persisted security settings.
func (c Config) ValidateDeploymentMode() error {
	mode := c.EffectiveDeploymentMode()
	switch mode {
	case DeploymentLocal, DeploymentPrivate, DeploymentPublic:
		if mode == DeploymentLocal && !c.IsLoopbackBind() && !c.AllowInsecureNonLoopback {
			return fmt.Errorf("DEPLOYMENT_MODE=local requires a loopback HOSTNAME")
		}
		return nil
	case "":
		if c.AllowInsecureNonLoopback {
			return nil
		}
		return fmt.Errorf("non-loopback HOSTNAME requires DEPLOYMENT_MODE=private or public")
	default:
		return fmt.Errorf("invalid DEPLOYMENT_MODE %q (expected local, private, or public)", c.DeploymentMode)
	}
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

func envCSV(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
