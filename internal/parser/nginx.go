package parser

import (
	"fmt"
	"regexp"
)

// Combined log format:
// $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
var combinedRegex = regexp.MustCompile(
	`^(\S+)\s+\S+\s+\S+\s+\[([^\]]+)\]\s+"([^"]+)"\s+(\d+)\s+(\d+|-)\s+"([^"]*?)"\s+"([^"]*?)"`,
)

// Extended combined with virtual host:
// $host $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
var vhostCombinedRegex = regexp.MustCompile(
	`^(\S+)\s+(\S+)\s+\S+\s+\S+\s+\[([^\]]+)\]\s+"([^"]+)"\s+(\d+)\s+(\d+|-)\s+"([^"]*?)"\s+"([^"]*?)"`,
)

// nginxParser parses Nginx access logs using one of the built-in presets.
type nginxParser struct {
	preset string
	re     *regexp.Regexp
	vhost  bool
}

func newNginxParser(preset string) (*nginxParser, error) {
	if preset == "" {
		preset = "combined"
	}
	switch preset {
	case "combined":
		return &nginxParser{preset: preset, re: combinedRegex}, nil
	case "vhost_combined":
		return &nginxParser{preset: preset, re: vhostCombinedRegex, vhost: true}, nil
	default:
		return nil, fmt.Errorf("unknown nginx preset: %q", preset)
	}
}

func (p *nginxParser) Name() string { return "nginx:" + p.preset }

func (p *nginxParser) Parse(line string) (*LogEntry, error) {
	m := p.re.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("line does not match nginx %s format", p.preset)
	}
	if p.vhost {
		return parseVHostCombined(m)
	}
	return parseCombined(m)
}
