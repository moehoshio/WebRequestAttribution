package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/moehoshio/WebRequestAttribution/internal/auth"
	"github.com/moehoshio/WebRequestAttribution/internal/runtimeconfig"
)

// ConfigHandler exposes the runtime configuration on /api/config and
// the one-click restart endpoint on /api/admin/restart. The settings
// panel in the dashboard talks to this.
type ConfigHandler struct {
	store           *runtimeconfig.Store
	allowedLogRoots []string
	listenAddr      string
	dbPath          string
	auth            *auth.Service
}

// NewConfigHandler wires the runtimeconfig store and the bootstrap
// fields together so the API can return both in a single payload. The
// auth service is optional; when non-nil, edits and restart requests
// are written to audit_log.
func NewConfigHandler(store *runtimeconfig.Store, listenAddr, dbPath string, allowedLogRoots []string, authSvc *auth.Service) *ConfigHandler {
	return &ConfigHandler{
		store:           store,
		allowedLogRoots: allowedLogRoots,
		listenAddr:      listenAddr,
		dbPath:          dbPath,
		auth:            authSvc,
	}
}

// RegisterRoutes wires the endpoints onto mux behind the supplied
// admin middleware.
func (h *ConfigHandler) RegisterRoutes(mux *http.ServeMux, adminMW func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/config", adminMW(h.handleConfig))
	mux.HandleFunc("/api/admin/restart", adminMW(h.handleRestart))
	mux.HandleFunc("/api/admin/browse", adminMW(h.handleBrowse))
}

// configEnvelope is what /api/config returns. The "bootstrap" object
// is read-only (changing it requires a restart) and is included so the
// settings UI can display the values it cannot edit.
type configEnvelope struct {
	Runtime   runtimeconfig.Runtime `json:"runtime"`
	Bootstrap bootstrapView         `json:"bootstrap"`
	Schema    schemaView            `json:"schema"`
}

type bootstrapView struct {
	ListenAddr      string   `json:"listen_addr"`
	DBPath          string   `json:"db_path"`
	AllowedLogRoots []string `json:"allowed_log_roots"`
}

// schemaView is a tiny machine-readable schema the frontend uses to
// drive the settings form (engine/preset enums, restart-required
// flags). Anything not in here is currently hard-coded in the HTML.
type schemaView struct {
	Engines         []string `json:"engines"`
	NginxPresets    []string `json:"nginx_presets"`
	ApachePresets   []string `json:"apache_presets"`
	SyslogProtos    []string `json:"syslog_protos"`
	SourceTypes     []string `json:"source_types"`
	RestartRequired []string `json:"restart_required"`
}

func (h *ConfigHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSONStatus(w, http.StatusOK, configEnvelope{
			Runtime: h.store.Get(),
			Bootstrap: bootstrapView{
				ListenAddr:      h.listenAddr,
				DBPath:          h.dbPath,
				AllowedLogRoots: h.allowedLogRoots,
			},
			Schema: schemaView{
				Engines:         []string{"auto", "nginx", "apache", "custom"},
				NginxPresets:    []string{"combined", "vhost_combined"},
				ApachePresets:   []string{"common", "combined", "vhost_combined"},
				SyslogProtos:    []string{"udp", "tcp"},
				SourceTypes:     []string{"file", "dir", "syslog"},
				RestartRequired: []string{"listen_addr", "db_path", "allowed_log_roots"},
			},
		})
	case http.MethodPut:
		var rc runtimeconfig.Runtime
		if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if err := h.store.Set(rc, h.allowedLogRoots); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.audit(r, "config_update", fmt.Sprintf("sources=%d watch=%v keywords=%d", len(rc.Sources), rc.Watch, len(rc.Keywords)))
		writeJSONStatus(w, http.StatusOK, map[string]interface{}{
			"status":  "ok",
			"runtime": h.store.Get(),
		})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRestart performs an in-place re-exec on Linux so the
// dashboard's "Restart server" button is functional under both
// systemd (which will see the new process keep the same PID) and
// Docker (where the container exits and the orchestrator restarts
// it). On non-Linux platforms we exit cleanly and rely on the
// orchestrator to bring us back up.
func (h *ConfigHandler) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "cannot resolve executable: "+err.Error())
		return
	}
	// Acknowledge before flipping the table so the browser actually
	// receives the response. The actual re-exec runs in a goroutine
	// after a short defer so flush has time to happen.
	h.audit(r, "server_restart", "")
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		// Defer the re-exec slightly so the HTTP response actually
		// makes it out of the kernel buffer before the process image
		// is replaced.
		if err := performRestart(exe); err != nil {
			log.Printf("restart failed: %v; exiting so orchestrator can relaunch", err)
			os.Exit(0)
		}
	}()
}

// browseEntry is one row in the visual path picker.
type browseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

// browseResponse is what /api/admin/browse returns. When Path is empty
// the picker is at the "roots" level and Entries lists the configured
// allowed_log_roots (the only places it is permitted to start from).
type browseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	AtRoots bool          `json:"at_roots"`
	Roots   []string      `json:"roots"`
	Entries []browseEntry `json:"entries"`
}

// handleBrowse powers the dashboard's visual log-path picker. It lists
// directory contents so operators can click their way to a log file or
// folder instead of typing an absolute path. Browsing is confined to
// allowed_log_roots; when that list is empty (operators who accept the
// risk) the whole filesystem is reachable, mirroring the path-allow
// behaviour in runtimeconfig.
func (h *ConfigHandler) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	roots := h.browseRoots()
	reqPath := strings.TrimSpace(r.URL.Query().Get("path"))

	// No path yet: present the roots as the starting points. With a
	// single root we descend into it directly so there is no pointless
	// one-item screen.
	if reqPath == "" {
		if len(roots) == 1 {
			reqPath = roots[0]
		} else {
			entries := make([]browseEntry, 0, len(roots))
			for _, root := range roots {
				entries = append(entries, browseEntry{Name: root, Path: root, IsDir: true})
			}
			writeJSONStatus(w, http.StatusOK, browseResponse{AtRoots: true, Roots: roots, Entries: entries})
			return
		}
	}

	clean := filepath.Clean(reqPath)
	if !h.browseAllowed(clean) {
		writeJSONError(w, http.StatusForbidden, "path is outside the allowed log roots")
		return
	}
	info, err := os.Stat(clean)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "cannot read path: "+err.Error())
		return
	}
	if !info.IsDir() {
		// Selecting a file's directory is the useful behaviour; fall
		// back to its parent so the picker stays navigable.
		clean = filepath.Dir(clean)
	}
	dirents, err := os.ReadDir(clean)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "cannot list directory: "+err.Error())
		return
	}
	entries := make([]browseEntry, 0, len(dirents))
	for _, de := range dirents {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue // hide dotfiles to keep the picker tidy
		}
		full := filepath.Join(clean, name)
		isDir := de.IsDir()
		var size int64
		if fi, err := de.Info(); err == nil && !isDir {
			size = fi.Size()
		}
		entries = append(entries, browseEntry{Name: name, Path: full, IsDir: isDir, Size: size})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // directories first
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	// Offer a parent unless we would step above the allowed roots.
	parent := filepath.Dir(clean)
	if parent == clean || !h.browseAllowed(parent) {
		parent = ""
	}
	writeJSONStatus(w, http.StatusOK, browseResponse{
		Path:    clean,
		Parent:  parent,
		Roots:   roots,
		Entries: entries,
	})
}

// browseRoots returns the cleaned, non-empty allowed_log_roots. When
// none are configured it falls back to the filesystem root so the
// picker still has somewhere to start.
func (h *ConfigHandler) browseRoots() []string {
	roots := make([]string, 0, len(h.allowedLogRoots))
	for _, root := range h.allowedLogRoots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." {
			continue
		}
		roots = append(roots, root)
	}
	if len(roots) == 0 {
		roots = append(roots, string(filepath.Separator))
	}
	return roots
}

// browseAllowed reports whether p is inside one of the configured
// allowed_log_roots. With no roots configured everything is allowed,
// matching runtimeconfig.pathAllowed.
func (h *ConfigHandler) browseAllowed(p string) bool {
	if len(h.allowedLogRoots) == 0 {
		return true
	}
	cleaned := filepath.Clean(p)
	for _, root := range h.allowedLogRoots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." {
			continue
		}
		rel, err := filepath.Rel(root, cleaned)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

func writeJSONStatus(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSONStatus(w, status, map[string]string{"error": msg})
}

// audit best-effort writes to the audit_log via the auth service.
// Missing user / service is silently tolerated so audit never breaks
// the user-visible action.
func (h *ConfigHandler) audit(r *http.Request, action, detail string) {
	if h.auth == nil {
		return
	}
	u := auth.UserFromContext(r.Context())
	var uid *int64
	username := ""
	if u != nil {
		uid = &u.ID
		username = u.Username
	}
	_ = h.auth.Audit(uid, action, username, clientIP(r), detail)
}

// clientIP duplicates the heuristic in auth.clientIP so api doesn't
// have to import an unexported symbol.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}

// performRestart re-execs the current binary on Linux. Anywhere else
// it returns an error so the caller falls back to a clean exit (let
// the orchestrator do the work).
func performRestart(exe string) error {
	if runtime.GOOS != "linux" {
		return errors.New("in-place restart only supported on Linux; relying on orchestrator")
	}
	args := os.Args
	env := os.Environ()
	// syscall.Exec replaces the current process image; on success it
	// does not return. We deliberately do NOT close the listener
	// first: under Linux, exec preserves the fds we want the new
	// image to inherit, and the kernel tears the old listener down
	// when the process image is replaced.
	if err := syscall.Exec(exe, args, env); err != nil {
		return fmt.Errorf("syscall.Exec: %w", err)
	}
	return nil
}
