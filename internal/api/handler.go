package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moehoshio/WebRequestAttribution/internal/storage"
)

type Handler struct {
	store *storage.Store
}

func NewHandler(store *storage.Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/requests", h.handleRequests)
	mux.HandleFunc("/api/geo", h.handleGeo)
	mux.HandleFunc("/api/realtime", h.handleRealtime)
}

// RegisterRoutesWithMiddleware registers the API routes wrapped with the
// supplied middleware (e.g. auth.RequireAuth). Use this when the server
// has authentication enabled.
func (h *Handler) RegisterRoutesWithMiddleware(mux *http.ServeMux, mw func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/stats", mw(h.handleStats))
	mux.HandleFunc("/api/requests", mw(h.handleRequests))
	mux.HandleFunc("/api/geo", mw(h.handleGeo))
	mux.HandleFunc("/api/realtime", mw(h.handleRealtime))
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f := parseFilter(r)
	result, err := h.store.Stats(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

func (h *Handler) handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f := parseFilter(r)
	result, err := h.store.Query(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// handleGeo returns per-country request counts for the world map. Only
// requests whose IP has been geolocated (status "ok") are included; the
// same filters as /api/stats apply.
func (h *Handler) handleGeo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f := parseFilter(r)
	countries, err := h.store.GeoAggregate(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if countries == nil {
		countries = []storage.GeoCountItem{}
	}
	total := 0
	for _, c := range countries {
		total += c.Count
	}
	writeJSON(w, map[string]interface{}{
		"countries": countries,
		"total":     total,
	})
}

// handleRealtime returns per-minute request counts for the recent
// window so the dashboard can render a live-updating trend. The same
// filters as /api/stats apply; an optional "minutes" query param
// controls the window size (default 60, capped at 1440).
func (h *Handler) handleRealtime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f := parseFilter(r)
	minutes := 60
	if v := r.URL.Query().Get("minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minutes = n
		}
	}
	if minutes > 1440 {
		minutes = 1440
	}
	series := h.store.RequestsPerMinute(f, minutes)
	if series == nil {
		series = []storage.MinuteCountItem{}
	}
	writeJSON(w, map[string]interface{}{"series": series})
}

// splitFilterValues splits a filter parameter into a positive match and
// a list of exclusions. Each occurrence of the parameter may contain
// several comma-separated terms; a term prefixed with "!" excludes
// matching rows (several exclusions may be combined). The first
// positive term wins — positive matches are single-valued, as before.
func splitFilterValues(vals []string) (positive string, excludes []string) {
	for _, raw := range vals {
		for _, term := range strings.Split(raw, ",") {
			term = strings.TrimSpace(term)
			if term == "" {
				continue
			}
			if strings.HasPrefix(term, "!") {
				if v := strings.TrimSpace(term[1:]); v != "" {
					excludes = append(excludes, v)
				}
			} else if positive == "" {
				positive = term
			}
		}
	}
	return positive, excludes
}

func parseFilter(r *http.Request) storage.QueryFilter {
	q := r.URL.Query()
	f := storage.QueryFilter{}
	f.IP, f.ExcludeIP = splitFilterValues(q["ip"])
	f.Path, f.ExcludePath = splitFilterValues(q["path"])
	f.Domain, f.ExcludeDomain = splitFilterValues(q["domain"])
	f.Method, f.ExcludeMethod = splitFilterValues(q["method"])
	f.OS, f.ExcludeOS = splitFilterValues(q["os"])
	f.Browser, f.ExcludeBrowser = splitFilterValues(q["browser"])
	f.Query, f.ExcludeQuery = splitFilterValues(q["query"])
	f.Keyword, f.ExcludeKeyword = splitFilterValues(q["keyword"])

	statusPos, statusExcl := splitFilterValues(q["status"])
	if statusPos != "" {
		f.Status, _ = strconv.Atoi(statusPos)
	}
	for _, v := range statusExcl {
		if n, err := strconv.Atoi(v); err == nil {
			f.ExcludeStatus = append(f.ExcludeStatus, n)
		}
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	if v := q.Get("offset"); v != "" {
		f.Offset, _ = strconv.Atoi(v)
	}
	if v := q.Get("start"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.StartTime = &t
		}
	}
	if v := q.Get("end"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			f.EndTime = &end
		}
	}

	return f
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}
