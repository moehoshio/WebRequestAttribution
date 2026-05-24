package parser

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LogEntry represents a parsed nginx access log line.
type LogEntry struct {
	IP        string
	Timestamp time.Time
	Method    string
	Path      string
	Query     string
	Protocol  string
	Status    int
	BodySize  int
	Referer   string
	UserAgent string
	Domain    string
}

// Combined log format regex:
// $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
var combinedRegex = regexp.MustCompile(
	`^(\S+)\s+\S+\s+\S+\s+\[([^\]]+)\]\s+"([^"]+)"\s+(\d+)\s+(\d+)\s+"([^"]*?)"\s+"([^"]*?)"`,
)

// Extended combined with virtual host:
// $host $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
var vhostCombinedRegex = regexp.MustCompile(
	`^(\S+)\s+(\S+)\s+\S+\s+\S+\s+\[([^\]]+)\]\s+"([^"]+)"\s+(\d+)\s+(\d+)\s+"([^"]*?)"\s+"([^"]*?)"`,
)

func ParseLine(line string) (*LogEntry, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("empty line")
	}

	// Try vhost combined first
	if m := vhostCombinedRegex.FindStringSubmatch(line); m != nil {
		return parseVHostCombined(m)
	}

	// Try standard combined
	if m := combinedRegex.FindStringSubmatch(line); m != nil {
		return parseCombined(m)
	}

	return nil, fmt.Errorf("line does not match known format")
}

func parseCombined(m []string) (*LogEntry, error) {
	entry := &LogEntry{
		IP: m[1],
	}

	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[2])
	if err != nil {
		return nil, fmt.Errorf("parse time: %w", err)
	}
	entry.Timestamp = t

	parseRequest(entry, m[3])

	entry.Status, _ = strconv.Atoi(m[4])
	entry.BodySize, _ = strconv.Atoi(m[5])
	entry.Referer = m[6]
	entry.UserAgent = m[7]

	// Try to extract domain from referer
	if entry.Referer != "" && entry.Referer != "-" {
		if u, err := url.Parse(entry.Referer); err == nil {
			entry.Domain = u.Host
		}
	}

	return entry, nil
}

func parseVHostCombined(m []string) (*LogEntry, error) {
	entry := &LogEntry{
		Domain: m[1],
		IP:     m[2],
	}

	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[3])
	if err != nil {
		return nil, fmt.Errorf("parse time: %w", err)
	}
	entry.Timestamp = t

	parseRequest(entry, m[4])

	entry.Status, _ = strconv.Atoi(m[5])
	entry.BodySize, _ = strconv.Atoi(m[6])
	entry.Referer = m[7]
	entry.UserAgent = m[8]

	return entry, nil
}

func parseRequest(entry *LogEntry, request string) {
	parts := strings.Fields(request)
	if len(parts) >= 1 {
		entry.Method = parts[0]
	}
	if len(parts) >= 2 {
		fullPath := parts[1]
		if idx := strings.IndexByte(fullPath, '?'); idx >= 0 {
			entry.Path = fullPath[:idx]
			entry.Query = fullPath[idx+1:]
		} else {
			entry.Path = fullPath
		}
	}
	if len(parts) >= 3 {
		entry.Protocol = parts[2]
	}
}
