// Package geo provides best-effort, coarse (country / region level) IP
// geolocation for the request-origin world map. It is intentionally
// lightweight: results are resolved lazily in the background, cached in
// SQLite, and the upstream lookups go to a free, open data provider
// (ip-api.com by default). When the host has no outbound network the
// resolver simply makes no progress — the rest of the dashboard keeps
// working and the map shows whatever has already been resolved.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/moehoshio/WebRequestAttribution/internal/storage"
)

// DefaultEndpoint is a free, no-API-key geolocation service. The {ip}
// placeholder is replaced with the address being looked up. ip-api.com
// allows ~45 requests/minute from a single source for non-commercial
// use, which the resolver respects via its request interval.
const DefaultEndpoint = "http://ip-api.com/json/{ip}?fields=status,message,country,countryCode,regionName,city,lat,lon"

// Store is the subset of storage.Store the resolver needs. Declared as
// an interface so the package can be unit-tested without a real DB.
type Store interface {
	DistinctUnresolvedIPs(limit int) ([]string, error)
	UpsertGeo(g storage.GeoEntry) error
}

// Resolver looks up un-geolocated IPs in the background and writes the
// results into the cache. It is safe for concurrent use.
type Resolver struct {
	store    Store
	endpoint string
	client   *http.Client

	enabled atomic.Bool

	// scanInterval is how long the loop sleeps between batches; reqDelay
	// throttles individual upstream requests to stay within the
	// provider's rate limit.
	scanInterval time.Duration
	reqDelay     time.Duration
	batchSize    int
}

// Options configures a Resolver. Zero values fall back to sensible
// defaults.
type Options struct {
	Endpoint     string
	Enabled      bool
	ScanInterval time.Duration
	ReqDelay     time.Duration
	BatchSize    int
}

// New constructs a Resolver. It does not start the background loop;
// call Run for that.
func New(store Store, opts Options) *Resolver {
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	scan := opts.ScanInterval
	if scan <= 0 {
		scan = 30 * time.Second
	}
	delay := opts.ReqDelay
	if delay <= 0 {
		delay = 1500 * time.Millisecond
	}
	batch := opts.BatchSize
	if batch <= 0 {
		batch = 40
	}
	r := &Resolver{
		store:        store,
		endpoint:     endpoint,
		client:       &http.Client{Timeout: 8 * time.Second},
		scanInterval: scan,
		reqDelay:     delay,
		batchSize:    batch,
	}
	r.enabled.Store(opts.Enabled)
	return r
}

// SetEnabled toggles background resolution at runtime. The settings
// panel flips this via the runtime-config subscription.
func (r *Resolver) SetEnabled(v bool) { r.enabled.Store(v) }

// Enabled reports the current toggle state.
func (r *Resolver) Enabled() bool { return r.enabled.Load() }

// Run drives the background resolution loop until ctx is cancelled. It
// is intended to be launched in its own goroutine.
func (r *Resolver) Run(ctx context.Context) {
	// A short initial delay lets the rest of the server settle before
	// the first batch fires.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if r.enabled.Load() {
			if err := r.resolveBatch(ctx); err != nil {
				// Network errors are expected on isolated hosts; log at
				// most a terse note and keep looping so the map starts
				// working as soon as connectivity returns.
				log.Printf("geo: batch paused: %v", err)
			}
		}
		timer.Reset(r.scanInterval)
	}
}

// resolveBatch resolves up to batchSize unresolved IPs. It returns an
// error only when the very first upstream request fails (treated as "no
// connectivity") so the caller can back off; per-IP provider failures
// are cached as status="fail" and not surfaced.
func (r *Resolver) resolveBatch(ctx context.Context) error {
	ips, err := r.store.DistinctUnresolvedIPs(r.batchSize)
	if err != nil {
		return err
	}
	sentUpstream := 0
	for _, ip := range ips {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if entry, ok := localEntry(ip); ok {
			_ = r.store.UpsertGeo(entry)
			continue
		}
		entry, err := r.lookup(ctx, ip)
		if err != nil {
			// First upstream request of a batch failing almost always
			// means no network. Surface it so the loop backs off
			// instead of hammering a dead provider.
			if sentUpstream == 0 {
				return err
			}
			return nil
		}
		_ = r.store.UpsertGeo(entry)
		sentUpstream++
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.reqDelay):
		}
	}
	return nil
}

// apiResponse mirrors the fields requested from ip-api.com.
type apiResponse struct {
	Status      string  `json:"status"`
	Message     string  `json:"message"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

// lookup queries the upstream provider for a single IP.
func (r *Resolver) lookup(ctx context.Context, ip string) (storage.GeoEntry, error) {
	url := strings.ReplaceAll(r.endpoint, "{ip}", ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return storage.GeoEntry{}, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return storage.GeoEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return storage.GeoEntry{}, fmt.Errorf("geo provider HTTP %d", resp.StatusCode)
	}
	var ar apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return storage.GeoEntry{}, err
	}
	if ar.Status != "success" {
		// Provider could not resolve it (reserved range, etc.). Cache a
		// negative result so we don't keep retrying the same address.
		return storage.GeoEntry{IP: ip, Status: "fail"}, nil
	}
	return storage.GeoEntry{
		IP:          ip,
		CountryCode: ar.CountryCode,
		Country:     ar.Country,
		Region:      ar.RegionName,
		City:        ar.City,
		Lat:         ar.Lat,
		Lon:         ar.Lon,
		Status:      "ok",
	}, nil
}

// localEntry short-circuits addresses that must never be sent to a
// third-party provider (loopback, private, link-local, unspecified, or
// unparseable). They are cached as a "private" status so they are
// excluded from the map but not retried forever.
func localEntry(ip string) (storage.GeoEntry, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return storage.GeoEntry{IP: ip, Status: "fail"}, true
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() ||
		parsed.IsLinkLocalMulticast() || parsed.IsUnspecified() {
		return storage.GeoEntry{IP: ip, Country: "Local Network", CountryCode: "LAN", Status: "private"}, true
	}
	return storage.GeoEntry{}, false
}
