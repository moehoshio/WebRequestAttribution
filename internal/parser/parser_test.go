package parser

import (
	"testing"
	"time"
)

func TestParseCombinedFormat(t *testing.T) {
	line := `192.168.1.1 - - [10/Oct/2023:13:55:36 +0000] "GET /api/users?id=123 HTTP/1.1" 200 1234 "https://example.com/page" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"`
	entry, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.IP != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", entry.IP)
	}
	if entry.Method != "GET" {
		t.Errorf("expected method GET, got %s", entry.Method)
	}
	if entry.Path != "/api/users" {
		t.Errorf("expected path /api/users, got %s", entry.Path)
	}
	if entry.Query != "id=123" {
		t.Errorf("expected query id=123, got %s", entry.Query)
	}
	if entry.Status != 200 {
		t.Errorf("expected status 200, got %d", entry.Status)
	}
	if entry.BodySize != 1234 {
		t.Errorf("expected body size 1234, got %d", entry.BodySize)
	}
	if entry.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", entry.Domain)
	}

	expectedTime := time.Date(2023, 10, 10, 13, 55, 36, 0, time.UTC)
	if !entry.Timestamp.Equal(expectedTime) {
		t.Errorf("expected time %v, got %v", expectedTime, entry.Timestamp)
	}
}

func TestParseVHostCombinedFormat(t *testing.T) {
	line := `example.com 10.0.0.1 - admin [10/Oct/2023:14:00:00 +0000] "POST /login HTTP/2.0" 302 0 "-" "curl/7.68.0"`
	entry, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", entry.Domain)
	}
	if entry.IP != "10.0.0.1" {
		t.Errorf("expected IP 10.0.0.1, got %s", entry.IP)
	}
	if entry.Method != "POST" {
		t.Errorf("expected method POST, got %s", entry.Method)
	}
	if entry.Path != "/login" {
		t.Errorf("expected path /login, got %s", entry.Path)
	}
	if entry.Status != 302 {
		t.Errorf("expected status 302, got %d", entry.Status)
	}
}

func TestParseEmptyLine(t *testing.T) {
	_, err := ParseLine("")
	if err == nil {
		t.Error("expected error for empty line")
	}
}

func TestParseInvalidLine(t *testing.T) {
	_, err := ParseLine("this is not a valid log line")
	if err == nil {
		t.Error("expected error for invalid line")
	}
}

func TestOSInfo(t *testing.T) {
	tests := []struct {
		ua       string
		expected string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "Windows"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", "macOS"},
		{"Mozilla/5.0 (X11; Linux x86_64)", "Linux"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 14_0)", "iOS"},
		{"Mozilla/5.0 (Linux; Android 11)", "Android"},
		{"SomeBot/1.0", "Other"},
	}

	for _, tt := range tests {
		got := OSInfo(tt.ua)
		if got != tt.expected {
			t.Errorf("OSInfo(%q) = %s, want %s", tt.ua, got, tt.expected)
		}
	}
}

func TestBrowserInfo(t *testing.T) {
	tests := []struct {
		ua       string
		expected string
	}{
		{"Mozilla/5.0 Chrome/91.0", "Chrome"},
		{"Mozilla/5.0 Firefox/89.0", "Firefox"},
		{"Mozilla/5.0 Safari/537.36", "Safari"},
		{"Mozilla/5.0 Edg/91.0", "Edge"},
		{"Googlebot/2.1", "Bot"},
		{"", "Empty"},
	}

	for _, tt := range tests {
		got := BrowserInfo(tt.ua)
		if got != tt.expected {
			t.Errorf("BrowserInfo(%q) = %s, want %s", tt.ua, got, tt.expected)
		}
	}
}
