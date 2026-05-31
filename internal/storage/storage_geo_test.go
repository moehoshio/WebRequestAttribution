package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/moehoshio/WebRequestAttribution/internal/parser"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedRequest(t *testing.T, s *Store, ip string) {
	t.Helper()
	if err := s.Insert(&parser.LogEntry{
		IP:        ip,
		Timestamp: time.Now().UTC(),
		Method:    "GET",
		Path:      "/api",
		Protocol:  "HTTP/1.1",
		Status:    200,
		Domain:    "example.com",
	}, nil); err != nil {
		t.Fatalf("Insert(%s): %v", ip, err)
	}
}

func TestUpsertGetGeo(t *testing.T) {
	s := newTestStore(t)
	if _, ok, err := s.GetGeo("8.8.8.8"); err != nil || ok {
		t.Fatalf("expected no row, got ok=%v err=%v", ok, err)
	}
	in := GeoEntry{IP: "8.8.8.8", CountryCode: "US", Country: "United States", Region: "CA", City: "MV", Lat: 37.4, Lon: -122.0, Status: "ok"}
	if err := s.UpsertGeo(in); err != nil {
		t.Fatalf("UpsertGeo: %v", err)
	}
	got, ok, err := s.GetGeo("8.8.8.8")
	if err != nil || !ok {
		t.Fatalf("GetGeo: ok=%v err=%v", ok, err)
	}
	if got.CountryCode != "US" || got.Country != "United States" || got.Lat != 37.4 {
		t.Errorf("unexpected geo: %+v", got)
	}
	// Overwrite and confirm update.
	in.Country = "USA"
	if err := s.UpsertGeo(in); err != nil {
		t.Fatalf("UpsertGeo overwrite: %v", err)
	}
	got, _, _ = s.GetGeo("8.8.8.8")
	if got.Country != "USA" {
		t.Errorf("expected overwrite, got %q", got.Country)
	}
}

func TestUpsertGeoDefaultStatus(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertGeo(GeoEntry{IP: "9.9.9.9"}); err != nil {
		t.Fatalf("UpsertGeo: %v", err)
	}
	got, _, _ := s.GetGeo("9.9.9.9")
	if got.Status != "ok" {
		t.Errorf("expected default status ok, got %q", got.Status)
	}
}

func TestDistinctUnresolvedIPs(t *testing.T) {
	s := newTestStore(t)
	seedRequest(t, s, "8.8.8.8")
	seedRequest(t, s, "8.8.8.8")
	seedRequest(t, s, "1.1.1.1")
	// 8.8.8.8 is more frequent, so it should come first.
	ips, err := s.DistinctUnresolvedIPs(10)
	if err != nil {
		t.Fatalf("DistinctUnresolvedIPs: %v", err)
	}
	if len(ips) != 2 || ips[0] != "8.8.8.8" {
		t.Fatalf("unexpected unresolved set: %v", ips)
	}
	// Resolving 8.8.8.8 should drop it from the unresolved set.
	if err := s.UpsertGeo(GeoEntry{IP: "8.8.8.8", Status: "ok"}); err != nil {
		t.Fatalf("UpsertGeo: %v", err)
	}
	ips, _ = s.DistinctUnresolvedIPs(10)
	if len(ips) != 1 || ips[0] != "1.1.1.1" {
		t.Fatalf("expected only 1.1.1.1 unresolved, got %v", ips)
	}
}

func TestQueryIncludesCountry(t *testing.T) {
	s := newTestStore(t)
	seedRequest(t, s, "8.8.8.8")
	seedRequest(t, s, "1.1.1.1")
	if err := s.UpsertGeo(GeoEntry{IP: "8.8.8.8", CountryCode: "US", Country: "United States", Status: "ok"}); err != nil {
		t.Fatalf("UpsertGeo: %v", err)
	}
	res, err := s.Query(QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	byIP := map[string]RequestRow{}
	for _, r := range res.Rows {
		byIP[r.IP] = r
	}
	if byIP["8.8.8.8"].Country != "United States" || byIP["8.8.8.8"].CountryCode != "US" {
		t.Errorf("resolved row missing country: %+v", byIP["8.8.8.8"])
	}
	if byIP["1.1.1.1"].Country != "" {
		t.Errorf("unresolved row should have empty country, got %q", byIP["1.1.1.1"].Country)
	}
}

func TestGeoAggregate(t *testing.T) {
	s := newTestStore(t)
	seedRequest(t, s, "8.8.8.8")
	seedRequest(t, s, "8.8.8.8")
	seedRequest(t, s, "1.1.1.1")
	seedRequest(t, s, "10.0.0.1")
	s.UpsertGeo(GeoEntry{IP: "8.8.8.8", CountryCode: "US", Country: "United States", Lat: 37.4, Lon: -122.0, Status: "ok"})
	s.UpsertGeo(GeoEntry{IP: "1.1.1.1", CountryCode: "AU", Country: "Australia", Lat: -33.8, Lon: 151.2, Status: "ok"})
	s.UpsertGeo(GeoEntry{IP: "10.0.0.1", CountryCode: "LAN", Country: "Local Network", Status: "private"})

	items, err := s.GeoAggregate(QueryFilter{})
	if err != nil {
		t.Fatalf("GeoAggregate: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 countries (private excluded), got %d: %+v", len(items), items)
	}
	// Ordered by count desc → US (2) first.
	if items[0].CountryCode != "US" || items[0].Count != 2 {
		t.Errorf("unexpected top country: %+v", items[0])
	}
	if items[1].CountryCode != "AU" || items[1].Count != 1 {
		t.Errorf("unexpected second country: %+v", items[1])
	}
}
