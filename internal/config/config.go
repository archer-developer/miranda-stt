// Package config loads the service's YAML configuration and merges it over
// built-in defaults. See config/config.yaml.dist for the full field
// reference. Secrets are never stored here — only the names of the
// environment variables that hold them.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the root of the service's configuration tree.
type Config struct {
	// TCPAddr is the address the Wyoming TCP server listens on.
	TCPAddr string `yaml:"tcp_addr"`
	// GeminiAPIKeyEnv is the name of the env var holding the Gemini API key.
	GeminiAPIKeyEnv string `yaml:"gemini_api_key_env"`
	// GeminiModel is the Gemini model identifier.
	GeminiModel string `yaml:"gemini_model"`
	// Languages is the list of BCP-47 language codes advertised to Home Assistant.
	Languages []string `yaml:"languages"`
	// AudioDumpDir, when non-empty, enables WAV dumps of every session.
	AudioDumpDir string `yaml:"audio_dump_dir"`
	// GeminiTurnCompleteTimeout is the ms to wait for Gemini turn_complete after audio-stop.
	GeminiTurnCompleteTimeoutMs int `yaml:"gemini_turn_complete_timeout_ms"`
	Logging                     LoggingConfig `yaml:"logging"`
}

// LoggingConfig controls slog output level.
type LoggingConfig struct {
	// Level is one of "debug", "info", "warn", "error".
	Level string `yaml:"level"`
}

// Default returns the built-in configuration with safe, runnable values.
func Default() Config {
	return Config{
		TCPAddr:                     ":10300",
		GeminiAPIKeyEnv:             "GEMINI_API_KEY",
		GeminiModel:                 "models/gemini-3.5-transcribe-live",
		Languages:                   []string{"ru", "en"},
		AudioDumpDir:                "",
		GeminiTurnCompleteTimeoutMs: 1500,
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

// Load reads each YAML file in paths, in order, and merges it over Default().
// A missing file is silently skipped.
func Load(paths ...string) (Config, error) {
	cfg := Default()

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}

	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.TCPAddr == "" {
		return fmt.Errorf("config: tcp_addr must not be empty")
	}
	if c.GeminiAPIKeyEnv == "" {
		return fmt.Errorf("config: gemini_api_key_env must not be empty")
	}
	if c.GeminiModel == "" {
		return fmt.Errorf("config: gemini_model must not be empty")
	}
	if len(c.Languages) == 0 {
		return fmt.Errorf("config: languages must not be empty")
	}
	if c.GeminiTurnCompleteTimeoutMs <= 0 {
		return fmt.Errorf("config: gemini_turn_complete_timeout_ms must be positive")
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: logging.level must be one of debug|info|warn|error, got %q", c.Logging.Level)
	}
	return nil
}
