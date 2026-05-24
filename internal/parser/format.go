package parser

import (
	"fmt"
	"strings"
)

// FormatConfig selects a log parser engine and its preset/pattern.
//
// Engine values:
//   - "nginx":  use the built-in Nginx parser; Preset is one of "combined" or "vhost_combined".
//   - "apache": use the built-in Apache parser; Preset is one of "common", "combined" or "vhost_combined".
//   - "custom": use a user-supplied pattern (Nginx-style `$variable` tokens).
//   - "auto" or empty: try Nginx then Apache automatically.
type FormatConfig struct {
	Engine  string `json:"engine,omitempty"`
	Preset  string `json:"preset,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

// Parser parses one log line into a LogEntry.
type Parser interface {
	Parse(line string) (*LogEntry, error)
	Name() string
}

// New returns a Parser for the given format configuration.
func New(cfg FormatConfig) (Parser, error) {
	engine := strings.ToLower(strings.TrimSpace(cfg.Engine))
	if engine == "" {
		engine = "auto"
	}
	switch engine {
	case "auto":
		return autoParser{}, nil
	case "nginx":
		return newNginxParser(cfg.Preset)
	case "apache":
		return newApacheParser(cfg.Preset)
	case "custom":
		if strings.TrimSpace(cfg.Pattern) == "" {
			return nil, fmt.Errorf("custom parser requires a non-empty pattern")
		}
		return newCustomParser(cfg.Pattern)
	default:
		return nil, fmt.Errorf("unknown parser engine: %q", cfg.Engine)
	}
}

// autoParser tries Nginx then Apache combined presets.
type autoParser struct{}

func (autoParser) Name() string { return "auto" }

func (autoParser) Parse(line string) (*LogEntry, error) {
	return ParseLine(line)
}
