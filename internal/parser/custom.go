package parser

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// customParser parses lines according to a user-supplied pattern using
// Nginx-style `$variable` placeholders.
//
// Supported variables and their semantics:
//
//	$remote_addr                          → IP
//	$remote_user                          → ignored
//	$time_local                           → Timestamp (02/Jan/2006:15:04:05 -0700)
//	$msec                                 → Timestamp (unix seconds, may be fractional)
//	$request                              → "METHOD PATH PROTOCOL"
//	$request_method                       → Method
//	$request_uri / $uri                   → Path (+ query if present)
//	$status                               → Status
//	$body_bytes_sent / $bytes_sent        → BodySize
//	$http_referer                         → Referer
//	$http_user_agent                      → UserAgent
//	$http_host / $host / $server_name     → Domain
//	$request_time / $upstream_response_time → ignored (numeric)
//
// Unknown `$variable` tokens are accepted and matched non-greedily but their
// captured value is discarded. All non-token characters in the pattern are
// treated as regex-escaped literals.
//
// Apache `%`-style LogFormat tokens are not yet supported; see docs/TODO.md.
type customParser struct {
	pattern string
	re      *regexp.Regexp
	// fields[i] is the LogEntry field that capture group i+1 should populate,
	// or "" to discard.
	fields []string
}

// known maps an Nginx-style variable name (without the leading $) to a logical
// field identifier used internally.
var knownVariables = map[string]string{
	"remote_addr":            "ip",
	"remote_user":            "",
	"time_local":             "time_local",
	"msec":                   "msec",
	"request":                "request",
	"request_method":         "method",
	"request_uri":            "uri",
	"uri":                    "uri",
	"status":                 "status",
	"body_bytes_sent":        "bytes",
	"bytes_sent":             "bytes",
	"http_referer":           "referer",
	"http_user_agent":        "user_agent",
	"http_host":              "host",
	"host":                   "host",
	"server_name":            "host",
	"request_time":           "",
	"upstream_response_time": "",
}

// pattern token → regex fragment for the captured value.
var fieldRegex = map[string]string{
	"ip":         `(\S+)`,
	"time_local": `([^\]]+?)`,
	"msec":       `(\d+(?:\.\d+)?)`,
	"request":    `([^"]+)`,
	"method":     `(\S+)`,
	"uri":        `(\S+)`,
	"status":     `(\d+)`,
	"bytes":      `(\d+|-)`,
	"referer":    `([^"]*?)`,
	"user_agent": `([^"]*?)`,
	"host":       `(\S+)`,
}

func newCustomParser(pattern string) (*customParser, error) {
	var (
		sb     strings.Builder
		fields []string
		i      = 0
	)
	sb.WriteByte('^')
	for i < len(pattern) {
		c := pattern[i]
		if c == '$' && i+1 < len(pattern) && isVarStart(pattern[i+1]) {
			// Read identifier.
			j := i + 1
			for j < len(pattern) && isVarChar(pattern[j]) {
				j++
			}
			name := pattern[i+1 : j]
			i = j

			field, known := knownVariables[name]
			if !known {
				// Unknown variable: match non-greedily and discard.
				sb.WriteString(`(.*?)`)
				fields = append(fields, "")
				continue
			}
			if field == "" {
				// Recognised but ignored.
				sb.WriteString(`(\S+)`)
				fields = append(fields, "")
				continue
			}
			frag, ok := fieldRegex[field]
			if !ok {
				return nil, fmt.Errorf("internal: missing regex for field %q", field)
			}
			sb.WriteString(frag)
			fields = append(fields, field)
			continue
		}
		// Literal character; escape for regex.
		sb.WriteString(regexp.QuoteMeta(string(c)))
		i++
	}

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	return &customParser{pattern: pattern, re: re, fields: fields}, nil
}

func (p *customParser) Name() string { return "custom" }

func (p *customParser) Parse(line string) (*LogEntry, error) {
	m := p.re.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("line does not match custom pattern")
	}

	entry := &LogEntry{}
	for idx, field := range p.fields {
		val := m[idx+1]
		switch field {
		case "":
			// discard
		case "ip":
			entry.IP = val
		case "time_local":
			t, err := time.Parse("02/Jan/2006:15:04:05 -0700", val)
			if err != nil {
				return nil, fmt.Errorf("parse time_local %q: %w", val, err)
			}
			entry.Timestamp = t
		case "msec":
			sec, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return nil, fmt.Errorf("parse msec %q: %w", val, err)
			}
			whole := int64(sec)
			nsec := int64((sec - float64(whole)) * 1e9)
			entry.Timestamp = time.Unix(whole, nsec).UTC()
		case "request":
			parseRequest(entry, val)
		case "method":
			entry.Method = val
		case "uri":
			if idx := strings.IndexByte(val, '?'); idx >= 0 {
				entry.Path = val[:idx]
				entry.Query = val[idx+1:]
			} else {
				entry.Path = val
			}
		case "status":
			entry.Status, _ = strconv.Atoi(val)
		case "bytes":
			entry.BodySize = parseBodySize(val)
		case "referer":
			entry.Referer = val
		case "user_agent":
			entry.UserAgent = val
		case "host":
			entry.Domain = val
		}
	}

	if entry.Domain == "" && entry.Referer != "" && entry.Referer != "-" {
		if u, err := url.Parse(entry.Referer); err == nil {
			entry.Domain = u.Host
		}
	}
	return entry, nil
}

func isVarStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isVarChar(c byte) bool {
	return isVarStart(c) || (c >= '0' && c <= '9')
}
