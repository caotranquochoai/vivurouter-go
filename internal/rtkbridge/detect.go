package rtkbridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const PathEnv = "VIVUROUTER_RTK_PATH"

type Detection struct {
	Path      string `json:"path"`
	Found     bool   `json:"found"`
	Source    string `json:"source"`
	OS        string `json:"os"`
	Binary    string `json:"binary"`
	Message   string `json:"message,omitempty"`
	CanRunNow bool   `json:"can_run_now"`
}

func BinaryNameForOS(goos string) string {
	if goos == "windows" {
		return "rtk.exe"
	}
	return "rtk"
}

func Detect() Detection {
	return DetectFrom(filepath.Dir(os.Args[0]), os.Getenv(PathEnv), runtime.GOOS)
}

func DetectFrom(appDir string, envPath string, goos string) Detection {
	return DetectFromPathSource(appDir, envPath, "env", goos)
}

func DetectFromPathSource(appDir string, configuredPath string, configuredSource string, goos string) Detection {
	binary := BinaryNameForOS(goos)
	base := Detection{OS: goos, Binary: binary}
	if strings.TrimSpace(configuredPath) != "" {
		if strings.TrimSpace(configuredSource) == "" {
			configuredSource = "configured"
		}
		return checkCandidate(base, configuredPath, configuredSource)
	}
	if strings.TrimSpace(appDir) != "" {
		if detected := checkCandidate(base, filepath.Join(appDir, binary), "sibling"); detected.Found {
			return detected
		}
	}
	if path, err := exec.LookPath(binary); err == nil {
		return checkCandidate(base, path, "path")
	}
	base.Message = "rtk binary not found"
	return base
}

func checkCandidate(base Detection, path string, source string) Detection {
	out := base
	out.Path = path
	out.Source = source
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		out.Message = "rtk binary not found at " + path
		return out
	}
	out.Found = true
	out.CanRunNow = canRunBinaryOnOS(path, base.OS)
	if !out.CanRunNow {
		out.Message = "binary name does not match current OS; provide a native rtk binary"
	}
	return out
}

func canRunBinaryOnOS(path string, goos string) bool {
	name := strings.ToLower(filepath.Base(path))
	// Only an "rtk" binary may be invoked. This prevents pointing rtk_path at an
	// arbitrary executable (e.g. /bin/sh, calc.exe) and running it as the server.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem != "rtk" {
		return false
	}
	if goos == "windows" {
		return strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".cmd") || strings.HasSuffix(name, ".bat")
	}
	return !strings.HasSuffix(name, ".exe")
}
