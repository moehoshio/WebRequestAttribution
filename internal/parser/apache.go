package parser

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// Apache common log format:
// %h %l %u %t "%r" %>s %b
// e.g. 127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326
var apacheCommonRegex = regexp.MustCompile(
	`^(\S+)\s+\S+\s+\S+\s+\[([^\]]+)\]\s+"([^"]+)"\s+(\d+)\s+(\d+|-)\s*$`,
)

// Apache combined log format is identical on the wire to Nginx combined:
// %h %l %u %t "%r" %>s %b "%{Referer}i" "%{User-Agent}i"
// We reuse combinedRegex.

// Apache vhost_combined:
// %v:%p %h %l %u %t "%r" %>s %b "%{Referer}i" "%{User-Agent}i"
// The first token is host:port (e.g. example.com:80); we capture host and port separately.
var apacheVHostCombinedRegex = regexp.MustCompile(
	`^(\S+?)(?::(\d+))?\s+(\S+)\s+\S+\s+\S+\s+\[([^\]]+)\]\s+"([^"]+)"\s+(\d+)\s+(\d+|-)\s+"([^"]*?)"\s+"([^"]*?)"`,
)

// apacheParser parses Apache access logs using one of the built-in presets.
type apacheParser struct {
	preset string
	re     *regexp.Regexp
	kind   int // 0=common, 1=combined, 2=vhost_combined
}

func newApacheParser(preset string) (*apacheParser, error) {
	if preset == "" {
		preset = "combined"
	}
	switch preset {
	case "common":
		return &apacheParser{preset: preset, re: apacheCommonRegex, kind: 0}, nil
	case "combined":
		return &apacheParser{preset: preset, re: combinedRegex, kind: 1}, nil
	case "vhost_combined":
		return &apacheParser{preset: preset, re: apacheVHostCombinedRegex, kind: 2}, nil
	default:
		return nil, fmt.Errorf("unknown apache preset: %q", preset)
	}
}

func (p *apacheParser) Name() string { return "apache:" + p.preset }

func (p *apacheParser) Parse(line string) (*LogEntry, error) {
	m := p.re.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("line does not match apache %s format", p.preset)
	}
	switch p.kind {
	case 0:
		return parseApacheCommon(m)
	case 1:
		return parseCombined(m)
	case 2:
		return parseApacheVHostCombined(m)
	}
	return nil, fmt.Errorf("unreachable")
}

func parseApacheCommon(m []string) (*LogEntry, error) {
	entry := &LogEntry{IP: m[1]}

	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[2])
	if err != nil {
		return nil, fmt.Errorf("parse time: %w", err)
	}
	entry.Timestamp = t

	parseRequest(entry, m[3])
	entry.Status, _ = strconv.Atoi(m[4])
	entry.BodySize = parseBodySize(m[5])
	return entry, nil
}

func parseApacheVHostCombined(m []string) (*LogEntry, error) {
	// Groups: 1=host, 2=port (optional), 3=ip, 4=time, 5=request, 6=status, 7=bytes, 8=referer, 9=ua
	entry := &LogEntry{
		Domain: m[1],
		IP:     m[3],
	}

	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[4])
	if err != nil {
		return nil, fmt.Errorf("parse time: %w", err)
	}
	entry.Timestamp = t

	parseRequest(entry, m[5])
	entry.Status, _ = strconv.Atoi(m[6])
	entry.BodySize = parseBodySize(m[7])
	entry.Referer = m[8]
	entry.UserAgent = m[9]

	if entry.Referer != "" && entry.Referer != "-" && entry.Domain == "" {
		if u, err := url.Parse(entry.Referer); err == nil {
			entry.Domain = u.Host
		}
	}
	return entry, nil
}
