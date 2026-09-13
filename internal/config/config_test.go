package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoad_DebugLoggingDefaultsAudioDumpDir(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "config.yaml", "logging:\n  level: \"debug\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AudioDumpDir != defaultDebugAudioDumpDir {
		t.Errorf("AudioDumpDir = %q, want %q", cfg.AudioDumpDir, defaultDebugAudioDumpDir)
	}
}

func TestLoad_DebugLoggingDoesNotOverrideExplicitAudioDumpDir(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "config.yaml", "logging:\n  level: \"debug\"\naudio_dump_dir: \"custom/dir\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AudioDumpDir != "custom/dir" {
		t.Errorf("AudioDumpDir = %q, want %q", cfg.AudioDumpDir, "custom/dir")
	}
}

func TestLoad_NonDebugLoggingLeavesAudioDumpDirEmpty(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "config.yaml", "logging:\n  level: \"info\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AudioDumpDir != "" {
		t.Errorf("AudioDumpDir = %q, want empty", cfg.AudioDumpDir)
	}
}

func TestLoad_RejectsBadLoggingLevel(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "config.yaml", "logging:\n  level: \"verbose\"\n")

	if _, err := Load(path); err == nil {
		t.Fatal("Load: expected error for invalid logging level, got nil")
	}
}
