package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"github.com/moehoshio/WebRequestAttribution/internal/parser"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ip TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			query TEXT,
			protocol TEXT,
			status INTEGER,
			body_size INTEGER,
			referer TEXT,
			user_agent TEXT,
			domain TEXT,
			os TEXT,
			browser TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON requests(timestamp);
		CREATE INDEX IF NOT EXISTS idx_requests_path ON requests(path);
		CREATE INDEX IF NOT EXISTS idx_requests_ip ON requests(ip);
		CREATE INDEX IF NOT EXISTS idx_requests_domain ON requests(domain);
		CREATE INDEX IF NOT EXISTS idx_requests_status ON requests(status);

		CREATE TABLE IF NOT EXISTS keyword_hits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			keyword TEXT NOT NULL,
			request_id INTEGER NOT NULL,
			context TEXT,
			FOREIGN KEY (request_id) REFERENCES requests(id)
		);
		CREATE INDEX IF NOT EXISTS idx_keyword_hits_keyword ON keyword_hits(keyword);

		-- file_state tracks per-file ingestion position so the directory
		-- watcher (Phase 4) can survive restarts and detect rotation by
		-- inode change. The same table is used for one-shot compressed
		-- archive imports: completed archives are recorded with
		-- offset = size so they are not re-imported on the next scan.
		--
		-- fingerprint stores the first few dozen bytes of the file so
		-- rotation can be detected even on filesystems that reuse
		-- inodes when a file is removed and immediately recreated
		-- (e.g. tmpfs).
		CREATE TABLE IF NOT EXISTS file_state (
			path TEXT PRIMARY KEY,
			inode INTEGER NOT NULL,
			size INTEGER NOT NULL,
			offset INTEGER NOT NULL,
			mtime DATETIME NOT NULL,
			fingerprint BLOB NOT NULL DEFAULT x'',
			updated_at DATETIME NOT NULL
		);

		-- geo_cache stores a best-effort geolocation per client IP so the
		-- world map and country breakdown can be rendered without
		-- re-querying the upstream geo provider on every page load. The
		-- data is intentionally coarse (country, optional region) and is
		-- refreshed lazily by the background resolver. status is one of
		-- "ok" (resolved), "fail" (provider could not resolve it), or
		-- "private" (loopback / RFC1918 / reserved — never sent upstream).
		CREATE TABLE IF NOT EXISTS geo_cache (
			ip TEXT PRIMARY KEY,
			country_code TEXT,
			country TEXT,
			region TEXT,
			city TEXT,
			lat REAL,
			lon REAL,
			status TEXT NOT NULL DEFAULT 'ok',
			updated_at DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_geo_cache_country ON geo_cache(country_code);
	`)
	return err
}

// GeoEntry is a single cached geolocation record keyed by IP. Lat/Lon
// are only meaningful when Status == "ok".
type GeoEntry struct {
	IP          string  `json:"ip"`
	CountryCode string  `json:"country_code"`
	Country     string  `json:"country"`
	Region      string  `json:"region"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Status      string  `json:"status"`
}

// GetGeo returns the cached geolocation for ip, or ok=false when none
// has been resolved yet.
func (s *Store) GetGeo(ip string) (GeoEntry, bool, error) {
	var g GeoEntry
	err := s.db.QueryRow(
		`SELECT ip, country_code, country, region, city, lat, lon, status FROM geo_cache WHERE ip = ?`,
		ip,
	).Scan(&g.IP, &g.CountryCode, &g.Country, &g.Region, &g.City, &g.Lat, &g.Lon, &g.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GeoEntry{}, false, nil
		}
		return GeoEntry{}, false, err
	}
	return g, true, nil
}

// UpsertGeo writes (or overwrites) the cache row for g.IP.
func (s *Store) UpsertGeo(g GeoEntry) error {
	if g.Status == "" {
		g.Status = "ok"
	}
	_, err := s.db.Exec(
		`INSERT INTO geo_cache (ip, country_code, country, region, city, lat, lon, status, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(ip) DO UPDATE SET
		   country_code = excluded.country_code,
		   country = excluded.country,
		   region = excluded.region,
		   city = excluded.city,
		   lat = excluded.lat,
		   lon = excluded.lon,
		   status = excluded.status,
		   updated_at = excluded.updated_at`,
		g.IP, g.CountryCode, g.Country, g.Region, g.City, g.Lat, g.Lon, g.Status, time.Now().UTC(),
	)
	return err
}

// DistinctUnresolvedIPs returns up to limit client IPs that appear in
// the requests table but have no geo_cache row yet, most frequent
// first. The background resolver uses this to decide what to look up.
func (s *Store) DistinctUnresolvedIPs(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Query(
		`SELECT r.ip, COUNT(*) AS c FROM requests r
		 LEFT JOIN geo_cache g ON r.ip = g.ip
		 WHERE g.ip IS NULL AND r.ip <> ''
		 GROUP BY r.ip ORDER BY c DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ips []string
	for rows.Next() {
		var ip string
		var c int
		if err := rows.Scan(&ip, &c); err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, rows.Err()
}

// GeoCountItem aggregates request counts per country together with a
// representative coordinate (the mean of the resolved IPs' positions)
// so the frontend can place a single marker per country on the map.
type GeoCountItem struct {
	CountryCode string  `json:"country_code"`
	Country     string  `json:"country"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Count       int     `json:"count"`
}

// GeoAggregate returns per-country request counts for requests whose IP
// has been resolved to a real location (status = "ok"), honouring the
// same filters as Query/Stats.
func (s *Store) GeoAggregate(f QueryFilter) ([]GeoCountItem, error) {
	where, args := buildWhere(f, "r.")
	clause := "WHERE g.status = 'ok'"
	if where != "" {
		clause = where + " AND g.status = 'ok'"
	}
	query := `SELECT g.country_code, MAX(g.country), AVG(g.lat), AVG(g.lon), COUNT(*) AS c
		FROM requests r JOIN geo_cache g ON r.ip = g.ip ` + clause +
		` GROUP BY g.country_code ORDER BY c DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GeoCountItem
	for rows.Next() {
		var it GeoCountItem
		if err := rows.Scan(&it.CountryCode, &it.Country, &it.Lat, &it.Lon, &it.Count); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// FileState records per-file ingestion progress for the directory
// watcher. Inode is 0 on platforms that don't expose it (e.g. Windows);
// rotation detection then falls back to (size, mtime, fingerprint).
//
// Fingerprint holds the first FingerprintSize bytes of the file at the
// time the row was last written. A mismatch on a later stat signals
// the file was rewritten from the beginning (rotation by truncate or
// inode reuse).
type FileState struct {
	Path        string
	Inode       uint64
	Size        int64
	Offset      int64
	MTime       time.Time
	Fingerprint []byte
}

// FingerprintSize is the number of leading bytes the directory watcher
// reads to detect rotation on filesystems that reuse inodes.
const FingerprintSize = 64

// GetFileState returns the stored state for path, or ok=false when no
// row exists yet.
func (s *Store) GetFileState(path string) (FileState, bool, error) {
	var fs FileState
	err := s.db.QueryRow(
		`SELECT path, inode, size, offset, mtime, fingerprint FROM file_state WHERE path = ?`,
		path,
	).Scan(&fs.Path, &fs.Inode, &fs.Size, &fs.Offset, &fs.MTime, &fs.Fingerprint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FileState{}, false, nil
		}
		return FileState{}, false, err
	}
	return fs, true, nil
}

// UpsertFileState writes (or overwrites) the row for fs.Path.
func (s *Store) UpsertFileState(fs FileState) error {
	_, err := s.db.Exec(
		`INSERT INTO file_state (path, inode, size, offset, mtime, fingerprint, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   inode = excluded.inode,
		   size = excluded.size,
		   offset = excluded.offset,
		   mtime = excluded.mtime,
		   fingerprint = excluded.fingerprint,
		   updated_at = excluded.updated_at`,
		fs.Path, fs.Inode, fs.Size, fs.Offset, fs.MTime, fs.Fingerprint, time.Now().UTC(),
	)
	return err
}

func (s *Store) Insert(entry *parser.LogEntry, keywords []string) error {
	osName := parser.OSInfo(entry.UserAgent)
	browser := parser.BrowserInfo(entry.UserAgent)

	res, err := s.db.Exec(`
		INSERT INTO requests (ip, timestamp, method, path, query, protocol, status, body_size, referer, user_agent, domain, os, browser)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.IP, entry.Timestamp, entry.Method, entry.Path, entry.Query,
		entry.Protocol, entry.Status, entry.BodySize, entry.Referer,
		entry.UserAgent, entry.Domain, osName, browser,
	)
	if err != nil {
		return err
	}

	if len(keywords) > 0 {
		reqID, _ := res.LastInsertId()
		fullPath := entry.Path + "?" + entry.Query
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(fullPath), strings.ToLower(kw)) {
				_, err := s.db.Exec(`INSERT INTO keyword_hits (keyword, request_id, context) VALUES (?, ?, ?)`,
					kw, reqID, fullPath)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (s *Store) InsertBatch(entries []*parser.LogEntry, keywords []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmtReq, err := tx.Prepare(`
		INSERT INTO requests (ip, timestamp, method, path, query, protocol, status, body_size, referer, user_agent, domain, os, browser)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmtReq.Close()

	stmtKw, err := tx.Prepare(`INSERT INTO keyword_hits (keyword, request_id, context) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmtKw.Close()

	for _, entry := range entries {
		osName := parser.OSInfo(entry.UserAgent)
		browser := parser.BrowserInfo(entry.UserAgent)

		res, err := stmtReq.Exec(
			entry.IP, entry.Timestamp, entry.Method, entry.Path, entry.Query,
			entry.Protocol, entry.Status, entry.BodySize, entry.Referer,
			entry.UserAgent, entry.Domain, osName, browser,
		)
		if err != nil {
			return err
		}

		if len(keywords) > 0 {
			reqID, _ := res.LastInsertId()
			fullPath := entry.Path + "?" + entry.Query
			for _, kw := range keywords {
				if strings.Contains(strings.ToLower(fullPath), strings.ToLower(kw)) {
					if _, err := stmtKw.Exec(kw, reqID, fullPath); err != nil {
						return err
					}
				}
			}
		}
	}

	return tx.Commit()
}

// QueryFilter defines filters for querying requests.
type QueryFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
	IP        string
	Path      string
	Domain    string
	Method    string
	Status    int
	OS        string
	Browser   string
	Query     string
	Keyword   string
	Limit     int
	Offset    int
}

// RequestRow is a single request record.
type RequestRow struct {
	ID        int64     `json:"id"`
	IP        string    `json:"ip"`
	Timestamp time.Time `json:"timestamp"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Query     string    `json:"query"`
	Protocol  string    `json:"protocol"`
	Status    int       `json:"status"`
	BodySize  int       `json:"body_size"`
	Referer   string    `json:"referer"`
	UserAgent string    `json:"user_agent"`
	Domain    string    `json:"domain"`
	OS        string    `json:"os"`
	Browser   string    `json:"browser"`
	// Country / CountryCode are populated from geo_cache via a LEFT JOIN
	// when a geolocation has been resolved for the IP; empty otherwise.
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
}

type QueryResult struct {
	Total int          `json:"total"`
	Rows  []RequestRow `json:"rows"`
}

func (s *Store) Query(f QueryFilter) (*QueryResult, error) {
	where, args := buildWhere(f, "")

	// Count
	var total int
	countSQL := "SELECT COUNT(*) FROM requests" + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}

	// The row query joins geo_cache so each request can carry its
	// resolved country. Columns are qualified with the "r." prefix to
	// avoid ambiguity with geo_cache's own ip column.
	rowWhere, rowArgs := buildWhere(f, "r.")
	querySQL := `SELECT r.id, r.ip, r.timestamp, r.method, r.path, r.query, r.protocol, r.status, r.body_size, r.referer, r.user_agent, r.domain, r.os, r.browser,
		COALESCE(g.country, ''), COALESCE(g.country_code, '')
		FROM requests r LEFT JOIN geo_cache g ON r.ip = g.ip` + rowWhere + " ORDER BY r.timestamp DESC LIMIT ? OFFSET ?"
	rowArgs = append(rowArgs, limit, f.Offset)

	rows, err := s.db.Query(querySQL, rowArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RequestRow
	for rows.Next() {
		var r RequestRow
		if err := rows.Scan(&r.ID, &r.IP, &r.Timestamp, &r.Method, &r.Path, &r.Query, &r.Protocol, &r.Status, &r.BodySize, &r.Referer, &r.UserAgent, &r.Domain, &r.OS, &r.Browser, &r.Country, &r.CountryCode); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return &QueryResult{Total: total, Rows: results}, nil
}

// Stats returns aggregated statistics.
type StatsResult struct {
	TotalRequests int              `json:"total_requests"`
	TopPaths      []CountItem      `json:"top_paths"`
	TopIPs        []CountItem      `json:"top_ips"`
	TopDomains    []CountItem      `json:"top_domains"`
	TopOS         []CountItem      `json:"top_os"`
	TopBrowsers   []CountItem      `json:"top_browsers"`
	TopKeywords   []CountItem      `json:"top_keywords"`
	StatusCodes   []CountItem      `json:"status_codes"`
	RequestsPerDay []TimeCountItem `json:"requests_per_day"`
}

type CountItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type TimeCountItem struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func (s *Store) Stats(f QueryFilter) (*StatsResult, error) {
	where, args := buildWhere(f, "")
	result := &StatsResult{}

	// Total requests
	s.db.QueryRow("SELECT COUNT(*) FROM requests"+where, args...).Scan(&result.TotalRequests)

	// Top paths
	result.TopPaths = s.topN("path", where, args, 20)
	result.TopIPs = s.topN("ip", where, args, 20)
	result.TopDomains = s.topN("domain", where, args, 20)
	result.TopOS = s.topN("os", where, args, 10)
	result.TopBrowsers = s.topN("browser", where, args, 10)
	result.StatusCodes = s.topN("status", where, args, 10)

	// Top keywords
	kwWhere, kwArgs := buildKeywordWhere(f)
	result.TopKeywords = s.topNFrom("keyword_hits", "keyword", kwWhere, kwArgs, 20)

	// Requests per day
	result.RequestsPerDay = s.requestsPerDay(where, args)

	return result, nil
}

func (s *Store) topN(col, where string, args []interface{}, n int) []CountItem {
	query := fmt.Sprintf("SELECT %s, COUNT(*) as cnt FROM requests%s GROUP BY %s ORDER BY cnt DESC LIMIT ?", col, where, col)
	a := append(append([]interface{}{}, args...), n)
	rows, err := s.db.Query(query, a...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var items []CountItem
	for rows.Next() {
		var item CountItem
		rows.Scan(&item.Name, &item.Count)
		items = append(items, item)
	}
	return items
}

func (s *Store) topNFrom(table, col, where string, args []interface{}, n int) []CountItem {
	query := fmt.Sprintf("SELECT %s, COUNT(*) as cnt FROM %s%s GROUP BY %s ORDER BY cnt DESC LIMIT ?", col, table, where, col)
	a := append(append([]interface{}{}, args...), n)
	rows, err := s.db.Query(query, a...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var items []CountItem
	for rows.Next() {
		var item CountItem
		rows.Scan(&item.Name, &item.Count)
		items = append(items, item)
	}
	return items
}

func (s *Store) requestsPerDay(where string, args []interface{}) []TimeCountItem {
	query := "SELECT DATE(timestamp) as d, COUNT(*) as cnt FROM requests" + where + " GROUP BY d ORDER BY d DESC LIMIT 30"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var items []TimeCountItem
	for rows.Next() {
		var item TimeCountItem
		rows.Scan(&item.Date, &item.Count)
		items = append(items, item)
	}
	return items
}

func buildWhere(f QueryFilter, prefix string) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if f.StartTime != nil {
		conditions = append(conditions, prefix+"timestamp >= ?")
		args = append(args, *f.StartTime)
	}
	if f.EndTime != nil {
		conditions = append(conditions, prefix+"timestamp <= ?")
		args = append(args, *f.EndTime)
	}
	if f.IP != "" {
		conditions = append(conditions, prefix+"ip LIKE ?")
		args = append(args, "%"+f.IP+"%")
	}
	if f.Path != "" {
		conditions = append(conditions, prefix+"path LIKE ?")
		args = append(args, "%"+f.Path+"%")
	}
	if f.Domain != "" {
		conditions = append(conditions, prefix+"domain LIKE ?")
		args = append(args, "%"+f.Domain+"%")
	}
	if f.Method != "" {
		conditions = append(conditions, prefix+"method = ?")
		args = append(args, f.Method)
	}
	if f.Status > 0 {
		conditions = append(conditions, prefix+"status = ?")
		args = append(args, f.Status)
	}
	if f.OS != "" {
		conditions = append(conditions, prefix+"os = ?")
		args = append(args, f.OS)
	}
	if f.Browser != "" {
		conditions = append(conditions, prefix+"browser = ?")
		args = append(args, f.Browser)
	}
	if f.Query != "" {
		conditions = append(conditions, prefix+"query LIKE ?")
		args = append(args, "%"+f.Query+"%")
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func buildKeywordWhere(f QueryFilter) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if f.Keyword != "" {
		conditions = append(conditions, "keyword LIKE ?")
		args = append(args, "%"+f.Keyword+"%")
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB so other packages (e.g. internal/auth)
// can attach their own tables without re-opening the file.
func (s *Store) DB() *sql.DB { return s.db }
