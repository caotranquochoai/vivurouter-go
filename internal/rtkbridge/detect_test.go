package rtkbridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryNameForOS(t *testing.T) {
	if got := BinaryNameForOS("windows"); got != "rtk.exe" {
		t.Fatalf("windows binary = %q", got)
	}
	if got := BinaryNameForOS("linux"); got != "rtk" {
		t.Fatalf("linux binary = %q", got)
	}
	if got := BinaryNameForOS("darwin"); got != "rtk" {
		t.Fatalf("darwin binary = %q", got)
	}
}

func TestDetectFromEnvPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rtk")
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := DetectFrom("", path, "linux")
	if !detected.Found || detected.Source != "env" || !detected.CanRunNow {
		t.Fatalf("unexpected detection: %+v", detected)
	}
}

func TestDetectFromSibling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rtk.exe")
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := DetectFrom(dir, "", "windows")
	if !detected.Found || detected.Source != "sibling" || detected.Path != path {
		t.Fatalf("unexpected detection: %+v", detected)
	}
}

func TestDetectRejectsWindowsExeForLinuxRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rtk.exe")
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := DetectFrom("", path, "linux")
	if !detected.Found {
		t.Fatalf("expected env path to be found")
	}
	if detected.CanRunNow {
		t.Fatalf("linux should not accept rtk.exe as runnable: %+v", detected)
	}
}

func TestDetectRejectsNonRTKBinaryName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sh")
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := DetectFrom("", path, "linux")
	if !detected.Found {
		t.Fatalf("expected path to be found")
	}
	if detected.CanRunNow {
		t.Fatalf("non-rtk binary should not be runnable: %+v", detected)
	}
}

func TestDetectMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	detected := DetectFrom(t.TempDir(), "", "linux")
	if detected.Found {
		t.Fatalf("expected missing rtk: %+v", detected)
	}
}
