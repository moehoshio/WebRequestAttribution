package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/moehoshio/WebRequestAttribution/internal/storage"
)

// mockStore is an in-memory Store for exercising the resolver without a
// real database.
type mockStore struct {
	mu        sync.Mutex
	unresolved []string
	upserts   map[string]storage.GeoEntry
}

func newMockStore(unresolved ...string) *mockStore {
	return &mockStore{unresolved: unresolved, upserts: map[string]storage.GeoEntry{}}
}

func (m *mockStore) DistinctUnresolvedIPs(limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.unresolved))
	for i, ip := range m.unresolved {
		if i >= limit {
			break
		}
		out = append(out, ip)
	}
	return out, nil
}

func (m *mockStore) UpsertGeo(g storage.GeoEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserts[g.IP] = g
	return nil
}

func (m *mockStore) get(ip string) (storage.GeoEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.upserts[ip]
	return g, ok
}

func TestLocalEntryClassification(t *testing.T) {
	cases := []struct {
		ip       string
		wantOK   bool
		wantStat string
	}{
		{"127.0.0.1", true, "private"},
		{"::1", true, "private"},
		{"10.0.0.5", true, "private"},
		{"192.168.1.10", true, "private"},
		{"169.254.1.1", true, "private"},
		{"0.0.0.0", true, "private"},
		{"not-an-ip", true, "fail"},
		{"8.8.8.8", false, ""},
		{"1.1.1.1", false, ""},
	}
	for _, c := range cases {
		entry, ok := localEntry(c.ip)
		if ok != c.wantOK {
			t.Errorf("localEntry(%q) ok=%v, want %v", c.ip, ok, c.wantOK)
			continue
		}
		if ok && entry.Status != c.wantStat {
			t.Errorf("localEntry(%q) status=%q, want %q", c.ip, entry.Status, c.wantStat)
		}
	}
}

func TestResolveBatchPublicAndPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","country":"United States","countryCode":"US","regionName":"California","city":"Mountain View","lat":37.4,"lon":-122.0}`))
	}))
	defer srv.Close()

	store := newMockStore("8.8.8.8", "10.0.0.1")
	r := New(store, Options{
		Endpoint:  srv.URL + "/{ip}",
		Enabled:   true,
		ReqDelay:  time.Millisecond,
		BatchSize: 10,
	})
	if err := r.resolveBatch(context.Background()); err != nil {
		t.Fatalf("resolveBatch: %v", err)
	}
	pub, ok := store.get("8.8.8.8")
	if !ok || pub.Status != "ok" || pub.CountryCode != "US" || pub.Country != "United States" {
		t.Errorf("public IP not resolved correctly: %+v ok=%v", pub, ok)
	}
	priv, ok := store.get("10.0.0.1")
	if !ok || priv.Status != "private" {
		t.Errorf("private IP not classified locally: %+v ok=%v", priv, ok)
	}
}

func TestResolveBatchProviderFailureCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"fail","message":"reserved range"}`))
	}))
	defer srv.Close()

	store := newMockStore("203.0.113.1")
	r := New(store, Options{Endpoint: srv.URL + "/{ip}", Enabled: true, ReqDelay: time.Millisecond})
	if err := r.resolveBatch(context.Background()); err != nil {
		t.Fatalf("resolveBatch: %v", err)
	}
	got, ok := store.get("203.0.113.1")
	if !ok || got.Status != "fail" {
		t.Errorf("provider failure not cached as fail: %+v ok=%v", got, ok)
	}
}

func TestResolveBatchNoConnectivityBacksOff(t *testing.T) {
	store := newMockStore("8.8.8.8")
	// Point at an unroutable endpoint so the first upstream request errors.
	r := New(store, Options{Endpoint: "http://127.0.0.1:0/{ip}", Enabled: true, ReqDelay: time.Millisecond})
	r.client = &http.Client{Timeout: 200 * time.Millisecond}
	if err := r.resolveBatch(context.Background()); err == nil {
		t.Fatal("expected error when first upstream request fails")
	}
	if _, ok := store.get("8.8.8.8"); ok {
		t.Error("public IP should not be cached when the lookup fails")
	}
}

func TestSetEnabled(t *testing.T) {
	r := New(newMockStore(), Options{Enabled: false})
	if r.Enabled() {
		t.Fatal("expected disabled by default")
	}
	r.SetEnabled(true)
	if !r.Enabled() {
		t.Fatal("expected enabled after SetEnabled(true)")
	}
}
