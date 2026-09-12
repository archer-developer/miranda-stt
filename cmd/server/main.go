// Command server is the miranda-stt Wyoming STT server.
//
// It listens for TCP connections from Home Assistant on the Wyoming protocol,
// streams audio to the Google Gemini Multimodal Live API for real-time
// transcription, and returns the transcript back over TCP.
//
// Bootstrap: envfile.Load(.env) → list config/*.yaml → config.Load → build
// logger → check GEMINI_API_KEY → start Wyoming TCP server → graceful shutdown.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/archer-developer/miranda-stt/internal/config"
	"github.com/archer-developer/miranda-stt/internal/envfile"
	"github.com/archer-developer/miranda-stt/internal/wyoming"
)

const (
	dotEnvPath       = ".env"
	defaultConfigDir = "config"
	configDirEnv     = "STT_CONFIG_DIR"
	debugLogDir      = "logs"
	debugLogFile     = "debug.log"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := envfile.Load(dotEnvPath); err != nil {
		logger.Warn("failed to load .env, continuing with process environment", "error", err)
	}

	cfgDir := defaultConfigDir
	if v := os.Getenv(configDirEnv); v != "" {
		cfgDir = v
	}

	configPaths, err := configFilePaths(cfgDir, logger)
	if err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configPaths...)
	if err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}

	realLogger, closeLogger, err := buildLogger(cfg.Logging)
	if err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
	defer closeLogger()
	logger = realLogger

	if err := run(cfg, logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func configFilePaths(cfgDir string, logger *slog.Logger) ([]string, error) {
	entries, err := os.ReadDir(cfgDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn("config directory not found, using built-in defaults", "dir", cfgDir)
			return nil, nil
		}
		return nil, fmt.Errorf("main: read config dir %s: %w", cfgDir, err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		paths = append(paths, filepath.Join(cfgDir, entry.Name()))
	}

	logger.Info("config files discovered", "dir", cfgDir, "paths", paths)
	return paths, nil
}

func run(cfg config.Config, logger *slog.Logger) error {
	apiKey := os.Getenv(cfg.GeminiAPIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("main: environment variable %s (named by gemini_api_key_env) is not set — refusing to start", cfg.GeminiAPIKeyEnv)
	}

	srv, err := wyoming.NewServer(
		cfg.TCPAddr,
		logger,
		apiKey,
		cfg.GeminiModel,
		cfg.Languages,
		cfg.AudioDumpDir,
		cfg.GeminiTurnCompleteTimeoutMs,
	)
	if err != nil {
		return fmt.Errorf("main: create wyoming server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("miranda-stt ready", "addr", srv.Addr(), "model", cfg.GeminiModel)

	if err := srv.Serve(ctx); err != nil {
		return fmt.Errorf("main: serve: %w", err)
	}
	return nil
}

func buildLogger(cfg config.LoggingConfig) (*slog.Logger, func(), error) {
	noop := func() {}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, noop, fmt.Errorf("main: invalid logging.level %q: %w", cfg.Level, err)
	}

	stdoutLevel := level
	if stdoutLevel < slog.LevelInfo {
		stdoutLevel = slog.LevelInfo
	}
	stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: stdoutLevel})

	if level > slog.LevelDebug {
		return slog.New(stdoutHandler), noop, nil
	}

	if err := os.MkdirAll(debugLogDir, 0o755); err != nil {
		return nil, noop, fmt.Errorf("main: create debug log dir: %w", err)
	}
	debugPath := filepath.Join(debugLogDir, debugLogFile)
	debugWriter, err := os.OpenFile(debugPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, noop, fmt.Errorf("main: open debug log %s: %w", debugPath, err)
	}
	debugHandler := slog.NewTextHandler(debugWriter, &slog.HandlerOptions{Level: slog.LevelDebug})

	h := &levelSplitHandler{stdout: stdoutHandler, debugFile: debugHandler}
	return slog.New(h), func() { _ = debugWriter.Close() }, nil
}

type levelSplitHandler struct {
	stdout    slog.Handler
	debugFile slog.Handler
}

func (h *levelSplitHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.stdout.Enabled(ctx, level) || h.debugFile.Enabled(ctx, level)
}

func (h *levelSplitHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelInfo {
		return h.debugFile.Handle(ctx, r)
	}
	return h.stdout.Handle(ctx, r)
}

func (h *levelSplitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelSplitHandler{
		stdout:    h.stdout.WithAttrs(attrs),
		debugFile: h.debugFile.WithAttrs(attrs),
	}
}

func (h *levelSplitHandler) WithGroup(name string) slog.Handler {
	return &levelSplitHandler{
		stdout:    h.stdout.WithGroup(name),
		debugFile: h.debugFile.WithGroup(name),
	}
}
