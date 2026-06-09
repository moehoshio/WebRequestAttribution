package storage

import (
	"testing"
	"time"

	"github.com/moehoshio/WebRequestAttribution/internal/parser"
)

// userAgents maps the browser names asserted in the tests to UA strings
// that parser.BrowserInfo classifies accordingly.
var userAgents = map[string]string{
	"Chrome": "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
	"Bot":    "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
}

func seedEntry(t *testing.T, s *Store, path string, status int, browser string) {
	t.Helper()
	if err := s.Insert(&parser.LogEntry{
		IP:        "1.2.3.4",
		Timestamp: time.Now().UTC(),
		Method:    "GET",
		Path:      path,
		Protocol:  "HTTP/1.1",
		Status:    status,
		Domain:    "example.com",
		UserAgent: userAgents[browser],
	}, nil); err != nil {
		t.Fatalf("Insert(%s): %v", path, err)
	}
}

func TestQueryExcludeFilters(t *testing.T) {
	s := newTestStore(t)
	seedEntry(t, s, "/api/users", 200, "Chrome")
	seedEntry(t, s, "/health", 200, "Bot")
	seedEntry(t, s, "/metrics", 404, "Bot")

	// Single exclusion.
	res, err := s.Query(QueryFilter{ExcludePath: []string{"/health"}, Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("exclude /health: expected 2 rows, got %d", res.Total)
	}

	// Multiple exclusions combine (AND).
	res, err = s.Query(QueryFilter{ExcludePath: []string{"/health", "/metrics"}, Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Total != 1 || res.Rows[0].Path != "/api/users" {
		t.Fatalf("exclude both: expected only /api/users, got %+v", res.Rows)
	}

	// Exclusions across different fields, mixed with a positive filter.
	res, err = s.Query(QueryFilter{Path: "/", ExcludeStatus: []int{404}, ExcludeBrowser: []string{"Chrome"}, Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Total != 1 || res.Rows[0].Path != "/health" {
		t.Fatalf("mixed filters: expected only /health, got %+v", res.Rows)
	}
}

func TestStatsExcludeFilters(t *testing.T) {
	s := newTestStore(t)
	seedEntry(t, s, "/api/users", 200, "Chrome")
	seedEntry(t, s, "/health", 200, "Bot")

	st, err := s.Stats(QueryFilter{ExcludeBrowser: []string{"Bot"}})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.TotalRequests != 1 {
		t.Fatalf("expected 1 request after excluding Bot, got %d", st.TotalRequests)
	}
}
