package parser

import (
	"testing"
	"time"
)

func TestApacheCommonFormat(t *testing.T) {
	p, err := New(FormatConfig{Engine: "apache", Preset: "common"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	line := `127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.IP != "127.0.0.1" {
		t.Errorf("IP = %q", entry.IP)
	}
	if entry.Method != "GET" {
		t.Errorf("Method = %q", entry.Method)
	}
	if entry.Path != "/apache_pb.gif" {
		t.Errorf("Path = %q", entry.Path)
	}
	if entry.Status != 200 {
		t.Errorf("Status = %d", entry.Status)
	}
	if entry.BodySize != 2326 {
		t.Errorf("BodySize = %d", entry.BodySize)
	}
	want := time.Date(2000, 10, 10, 13, 55, 36, 0, time.FixedZone("", -7*3600))
	if !entry.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v", entry.Timestamp)
	}
}

func TestApacheCommonDashBytes(t *testing.T) {
	p, _ := New(FormatConfig{Engine: "apache", Preset: "common"})
	line := `127.0.0.1 - - [10/Oct/2000:13:55:36 +0000] "GET / HTTP/1.0" 304 -`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.BodySize != 0 {
		t.Errorf("BodySize = %d, want 0", entry.BodySize)
	}
}

func TestApacheCombined(t *testing.T) {
	p, _ := New(FormatConfig{Engine: "apache", Preset: "combined"})
	line := `192.168.1.1 - - [10/Oct/2023:13:55:36 +0000] "GET /api HTTP/1.1" 200 100 "https://example.com/" "curl/7.68.0"`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.Domain != "example.com" {
		t.Errorf("Domain = %q", entry.Domain)
	}
	if entry.UserAgent != "curl/7.68.0" {
		t.Errorf("UserAgent = %q", entry.UserAgent)
	}
}

func TestApacheVHostCombined(t *testing.T) {
	p, _ := New(FormatConfig{Engine: "apache", Preset: "vhost_combined"})
	line := `example.com:80 10.0.0.1 - - [10/Oct/2023:14:00:00 +0000] "POST /login HTTP/2.0" 302 0 "-" "curl/7.68.0"`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.Domain != "example.com" {
		t.Errorf("Domain = %q", entry.Domain)
	}
	if entry.IP != "10.0.0.1" {
		t.Errorf("IP = %q", entry.IP)
	}
	if entry.Method != "POST" {
		t.Errorf("Method = %q", entry.Method)
	}
}

func TestCustomParserNginxStyle(t *testing.T) {
	p, err := New(FormatConfig{
		Engine:  "custom",
		Pattern: `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"`,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	line := `1.2.3.4 - - [10/Oct/2023:13:55:36 +0000] "GET /x HTTP/1.1" 200 5 "-" "Test/1.0"`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.IP != "1.2.3.4" || entry.Path != "/x" || entry.Status != 200 || entry.BodySize != 5 || entry.UserAgent != "Test/1.0" {
		t.Errorf("entry = %+v", entry)
	}
}

func TestCustomParserCompact(t *testing.T) {
	p, err := New(FormatConfig{
		Engine:  "custom",
		Pattern: `$remote_addr|$status|$request_method|$request_uri`,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entry, err := p.Parse("8.8.8.8|404|GET|/missing?x=1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.IP != "8.8.8.8" || entry.Status != 404 || entry.Method != "GET" || entry.Path != "/missing" || entry.Query != "x=1" {
		t.Errorf("entry = %+v", entry)
	}
}

func TestCustomParserUnknownVariableTolerated(t *testing.T) {
	p, err := New(FormatConfig{
		Engine:  "custom",
		Pattern: `$remote_addr $unknown_thing $status`,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entry, err := p.Parse("1.1.1.1 hello 200")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.IP != "1.1.1.1" || entry.Status != 200 {
		t.Errorf("entry = %+v", entry)
	}
}

func TestCustomParserEmptyPatternRejected(t *testing.T) {
	if _, err := New(FormatConfig{Engine: "custom", Pattern: ""}); err == nil {
		t.Error("expected error for empty custom pattern")
	}
}

func TestNginxParserStillWorks(t *testing.T) {
	p, err := New(FormatConfig{Engine: "nginx", Preset: "combined"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	line := `192.168.1.1 - - [10/Oct/2023:13:55:36 +0000] "GET / HTTP/1.1" 200 1234 "-" "UA"`
	if _, err := p.Parse(line); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestUnknownEngineRejected(t *testing.T) {
	if _, err := New(FormatConfig{Engine: "bogus"}); err == nil {
		t.Error("expected error for unknown engine")
	}
}
