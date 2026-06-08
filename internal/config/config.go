package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/moehoshio/WebRequestAttribution/internal/parser"
	"github.com/moehoshio/WebRequestAttribution/internal/runtimeconfig"
)

// SourceType identifies the kind of log source.
type SourceType string

const (
	SourceFile   SourceType = "file"
	SourceDir    SourceType = "dir"
	SourceSyslog SourceType = "syslog"
)

// Source describes one place to ingest log lines from.
type Source struct {
	// Name is an optional human-readable identifier.
	Name string `json:"name,omitempty"`
	// Type is "file", "dir", or "syslog".
	Type SourceType `json:"type"`
	// Format describes how to parse lines from this source.
	Format parser.FormatConfig `json:"format"`

	// File- and dir-source fields.
	Path string `json:"path,omitempty"`
	// ReadCompressed enables reading rotated/archived `.gz` files when first
	// opening the source. Live tailing of compressed files is not supported.
	// Support for `.bz2`/`.xz` is tracked in docs/TODO.md.
	ReadCompressed bool `json:"read_compressed,omitempty"`

	// Dir-source fields. Pattern is a filepath-glob matched against
	// the basename of discovered files (e.g. `access*.log*`).
	// Recursive enables descending into subdirectories.
	Pattern   string `json:"pattern,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`

	// Syslog-source fields.
	Addr  string `json:"addr,omitempty"`
	Proto string `json:"proto,omitempty"` // "udp", "tcp", or "both"
}

// Config is the top-level application configuration. Only the fields
// in this struct (listen_addr, db_path, auth.bootstrap_admin,
// allowed_log_roots) are considered bootstrap config: they are read
// once at startup and changing them requires a restart. Runtime-tunable
// fields (sources, keywords, watch toggle) are stored in the database
// and edited via the settings panel; see docs/ROADMAP.md (Phase 3).
//
// For convenience, the bootstrap file may also contain initial values
// for the runtime fields; they are used to seed the database on first
// launch and ignored thereafter.
type Config struct {
	// HTTP server listen address.
	ListenAddr string `json:"listen_addr"`
	// SQLite database path.
	DBPath string `json:"db_path"`
	// AllowedLogRoots restricts which filesystem prefixes the settings
	// panel is allowed to point file sources at. Empty disables the
	// check (anything goes). Operators exposing the dashboard to a
	// network are strongly encouraged to set this.
	AllowedLogRoots []string `json:"allowed_log_roots,omitempty"`
	// Auth contains bootstrap settings for the account system. See
	// docs/ROADMAP.md (Phase 2).
	Auth AuthConfig `json:"auth"`
	// Geo contains settings for IP geolocation (the world map). See
	// internal/geo.
	Geo GeoConfig `json:"geo"`

	// --- runtime seed values (only used when the runtime_config row
	// is empty, i.e. first launch). ---

	// Whether to start watchers on launch.
	Watch bool `json:"watch"`
	// Keywords to track in request paths and query strings.
	Keywords []string `json:"keywords"`
	// Sources is the list of log inputs to ingest from.
	Sources []Source `json:"sources"`
}

// AuthConfig holds settings consumed at startup by the auth package.
type AuthConfig struct {
	// BootstrapAdmin creates the named admin user on first launch when
	// the users table is empty. Both fields are required to trigger
	// the bootstrap; otherwise the operator must create the first user
	// out-of-band (e.g. by inserting into SQLite directly).
	BootstrapAdmin *BootstrapAdmin `json:"bootstrap_admin,omitempty"`
	// RequireAccount opts into "account mode": the dashboard must be
	// gated behind a login. When this is true but no usable account
	// exists yet (no users in the database and no bootstrap_admin
	// password supplied), the server creates an "admin" account with a
	// randomly-generated password and prints it to the backend log so
	// the operator can sign in. The default (false) keeps the
	// friendlier no-account mode where the first visitor configures the
	// server and creates the first account from the UI.
	RequireAccount bool `json:"require_account,omitempty"`
	// BcryptCost overrides the bcrypt cost parameter. 0 → default (10).
	BcryptCost int `json:"bcrypt_cost,omitempty"`
	// SessionTTLHours overrides the session lifetime. 0 → 24 hours.
	SessionTTLHours int `json:"session_ttl_hours,omitempty"`
	// CookieSecure issues cookies with the Secure attribute (HTTPS only).
	CookieSecure bool `json:"cookie_secure,omitempty"`
}

// GeoConfig holds settings for the background IP geolocation resolver.
type GeoConfig struct {
	// Enabled is a pointer so "absent" can be distinguished from
	// "explicitly false". When nil the feature defaults to on. The live
	// value is stored in the runtime config and editable from the
	// settings panel; this only seeds the first launch.
	Enabled *bool `json:"enabled,omitempty"`
	// Endpoint overrides the geolocation provider URL template. The
	// "{ip}" placeholder is substituted with the address being looked
	// up. Empty uses geo.DefaultEndpoint.
	Endpoint string `json:"endpoint,omitempty"`
}

// GeoEnabled reports whether geolocation should be seeded as enabled.
// Absent (nil) defaults to true.
func (g GeoConfig) GeoEnabled() bool {
	return g.Enabled == nil || *g.Enabled
}

// BootstrapAdmin describes the initial admin account created on first
// launch.
type BootstrapAdmin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// DefaultConfig returns a Config populated with sensible defaults.
//
// Out of the box we seed a single directory source that recursively
// scans the current working folder for log files (`*.log*`) and turn
// Watch on, so a freshly launched binary starts surfacing data without
// any manual configuration. A dir source matched by a glob does not
// spam "file not found" errors when nothing matches — it simply finds
// no files — so this is safe even on hosts without the expected layout.
// The operator can still refine or remove the source from the Settings
// panel afterwards.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr: ":8080",
		DBPath:     "./data/stats.db",
		Watch:      true,
		Keywords:   []string{},
		Sources: []Source{
			{
				Name:      "current-folder",
				Type:      SourceDir,
				Path:      ".",
				Pattern:   "*.log*",
				Recursive: true,
				Format:    parser.FormatConfig{Engine: "auto"},
			},
		},
	}
}

// Load reads configuration from disk. A missing file is treated as a
// request for defaults; the defaults are then written back to `path`
// so the operator has a starting point to edit.
//
// If writing the auto-generated file fails (e.g. read-only filesystem)
// the in-memory defaults are still returned; the auto-generation is a
// best-effort convenience, not a hard requirement.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Best-effort: drop a starter file next to the binary so
			// users have something to edit. Errors are logged via the
			// returned cfg's saved-path field is unnecessary; failure
			// to write just means we run with in-memory defaults.
			_ = cfg.Save(path)
			return cfg, nil
		}
		return nil, err
	}
	// Replace defaults entirely when a file is provided so unset fields are
	// explicit rather than silently merged.
	cfg = &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the configuration for obvious mistakes.
func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	if c.DBPath == "" {
		c.DBPath = "./data/stats.db"
	}
	for i, s := range c.Sources {
		switch s.Type {
		case SourceFile:
			if s.Path == "" {
				return fmt.Errorf("sources[%d]: file source requires \"path\"", i)
			}
		case SourceDir:
			if s.Path == "" {
				return fmt.Errorf("sources[%d]: dir source requires \"path\"", i)
			}
		case SourceSyslog:
			if s.Addr == "" {
				return fmt.Errorf("sources[%d]: syslog source requires \"addr\"", i)
			}
			if s.Proto == "" {
				c.Sources[i].Proto = "udp"
			}
		case "":
			return fmt.Errorf("sources[%d]: missing \"type\"", i)
		default:
			return fmt.Errorf("sources[%d]: unknown type %q", i, s.Type)
		}
	}
	return nil
}

// Save writes the configuration to disk as indented JSON. Any missing
// parent directories are created with 0755 permissions.
func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0644)
}

// RuntimeSeed converts the bootstrap-file's runtime-tunable fields
// (watch, keywords, sources) into a runtimeconfig.Runtime value. It is
// only consulted when the runtime_config table is empty (first launch).
func (c *Config) RuntimeSeed() runtimeconfig.Runtime {
	srcs := make([]runtimeconfig.Source, 0, len(c.Sources))
	for _, s := range c.Sources {
		srcs = append(srcs, runtimeconfig.Source{
			Name:           s.Name,
			Type:           runtimeconfig.SourceType(s.Type),
			Format:         s.Format,
			Path:           s.Path,
			ReadCompressed: s.ReadCompressed,
			Pattern:        s.Pattern,
			Recursive:      s.Recursive,
			Addr:           s.Addr,
			Proto:          s.Proto,
		})
	}
	if c.Keywords == nil {
		c.Keywords = []string{}
	}
	return runtimeconfig.Runtime{
		Watch:      c.Watch,
		Keywords:   append([]string(nil), c.Keywords...),
		Sources:    srcs,
		GeoEnabled: c.Geo.GeoEnabled(),
	}
}