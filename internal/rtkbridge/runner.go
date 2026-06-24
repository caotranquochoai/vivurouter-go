package rtkbridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/tokenopt"
)

const defaultMaxInputBytes = 2 * 1024 * 1024

type Runner struct {
	Path          string
	Timeout       time.Duration
	TempDir       string
	MaxInputBytes int
}

type CompactMode string

const (
	CompactModeText CompactMode = "text"
	CompactModeJSON CompactMode = "json"
	CompactModeLog  CompactMode = "log"
)

func NewRunner(path string) Runner {
	return Runner{Path: path, Timeout: 5 * time.Second, MaxInputBytes: defaultMaxInputBytes}
}

func (r Runner) Version(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r Runner) CompactToolResult(ctx context.Context, input string, opts tokenopt.Options) (tokenopt.Result, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return tokenopt.CompactToolResult(input, opts), nil
	}
	mode := CompactModeText
	if looksLikeJSON(trimmed) {
		mode = CompactModeJSON
	} else if looksLikeLog(input) {
		mode = CompactModeLog
	}
	return r.Compact(ctx, mode, input, opts)
}

func (r Runner) Compact(ctx context.Context, mode CompactMode, input string, opts tokenopt.Options) (tokenopt.Result, error) {
	if strings.TrimSpace(r.Path) == "" {
		return tokenopt.Result{}, errors.New("rtk path is empty")
	}
	if max := r.maxInputBytes(); len([]byte(input)) > max {
		return tokenopt.Result{}, errors.New("rtk input exceeds size limit")
	}
	file, cleanup, err := r.writeTempInput(input)
	if err != nil {
		return tokenopt.Result{}, err
	}
	defer cleanup()

	args := r.compactArgs(mode, file)
	out, err := r.run(ctx, args...)
	if err != nil {
		return tokenopt.Result{}, err
	}
	res := tokenopt.CompactToolResult(input, opts)
	if strings.TrimSpace(out) != "" {
		res = tokenopt.ResultFromCompactText(input, out, "rtk "+string(mode))
	}
	if !res.Applied {
		return res, nil
	}
	if opts.MaxChars > 0 && res.CompactChars > opts.MaxChars {
		fallback := tokenopt.CompactToolResult(res.Text, tokenopt.Options{MinChars: 0, MaxChars: opts.MaxChars, PreserveErrors: opts.PreserveErrors})
		if fallback.Applied {
			res = tokenopt.ResultFromCompactText(input, fallback.Text, "rtk "+string(mode)+" + native trim")
		}
	}
	return res, nil
}

func (r Runner) compactArgs(mode CompactMode, file string) []string {
	switch mode {
	case CompactModeJSON:
		return []string{"json", file}
	case CompactModeLog:
		return []string{"log", file}
	default:
		return []string{"read", file, "--level", "aggressive"}
	}
}

func (r Runner) run(ctx context.Context, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Path, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (r Runner) writeTempInput(input string) (string, func(), error) {
	dir := r.TempDir
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join(os.TempDir(), "vivurouter-rtk")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", func() {}, err
	}
	file, err := os.CreateTemp(dir, "rtk-input-*.txt")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := file.WriteString(input); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func (r Runner) maxInputBytes() int {
	if r.MaxInputBytes <= 0 {
		return defaultMaxInputBytes
	}
	return r.MaxInputBytes
}

func looksLikeJSON(input string) bool {
	return strings.HasPrefix(input, "{") || strings.HasPrefix(input, "[")
}

func looksLikeLog(input string) bool {
	lower := strings.ToLower(input)
	markers := []string{"error", "warn", "info", "debug", "trace", "fatal", "panic", "failed"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Count(input, "\n") >= 20
}
