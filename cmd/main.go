package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/moehoshio/WebRequestAttribution/internal/api"
	"github.com/moehoshio/WebRequestAttribution/internal/auth"
	"github.com/moehoshio/WebRequestAttribution/internal/config"
	"github.com/moehoshio/WebRequestAttribution/internal/geo"
	"github.com/moehoshio/WebRequestAttribution/internal/parser"
	"github.com/moehoshio/WebRequestAttribution/internal/runtimeconfig"
	"github.com/moehoshio/WebRequestAttribution/internal/storage"
	"github.com/moehoshio/WebRequestAttribution/internal/watcher"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	importFile := flag.String("import", "", "import a log file and exit")
	importFormatEngine := flag.String("import-format", "auto", "parser engine for -import (auto|nginx|apache|custom)")
	importPreset := flag.String("import-preset", "", "parser preset for -import (e.g. combined, common)")
	importPattern := flag.String("import-pattern", "", "custom pattern for -import when -import-format=custom")
	flag.Parse()

	// Auto-generate a starter config file when none exists at the
	// configured path. This makes "run the binary in an empty dir"
	// just work — the operator gets a file they can edit instead of
	// invisible in-memory defaults. config.Load() will write the file
	// itself; we only log here so the operator notices.
	configAutoCreated := false
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		configAutoCreated = true
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if configAutoCreated {
		if _, err := os.Stat(*configPath); err == nil {
			log.Printf("No config file found; wrote starter %s. Configure log sources via the settings panel.", *configPath)
		} else {
			log.Printf("No config file found and could not write %s (%v); running with in-memory defaults.", *configPath, err)
		}
	}

	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.Close()

	// If import mode, process file and exit. Keywords come from the
	// (persisted) runtime config so an import shares the same set as
	// the live watcher pipeline.
	if *importFile != "" {
		p, err := parser.New(parser.FormatConfig{
			Engine:  *importFormatEngine,
			Preset:  *importPreset,
			Pattern: *importPattern,
		})
		if err != nil {
			log.Fatalf("Invalid import format: %v", err)
		}
		rc, err := runtimeconfig.New(store.DB(), cfg.RuntimeSeed())
		if err != nil {
			log.Fatalf("Failed to init runtime config: %v", err)
		}
		count, err := importLogFile(store, *importFile, rc.Get().Keywords, p)
		if err != nil {
			log.Fatalf("Import failed: %v", err)
		}
		fmt.Printf("Imported %d records from %s\n", count, *importFile)
		return
	}

	// Start log watchers in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Runtime config lives in the database from Phase 3 onwards. The
	// file's sources/keywords/watch fields are only consulted on first
	// launch to seed the row.
	rcStore, err := runtimeconfig.New(store.DB(), cfg.RuntimeSeed())
	if err != nil {
		log.Fatalf("Failed to init runtime config: %v", err)
	}

	// Watcher manager owns source lifecycles. It subscribes to
	// runtime-config changes so the settings panel's "Save" button
	// can start/stop/restart sources without bouncing the process.
	mgr := watcher.NewManager(ctx, store)
	if err := mgr.Apply(rcStore.Get()); err != nil {
		log.Printf("initial watcher apply: %v", err)
	}
	rcStore.Subscribe(func(rc runtimeconfig.Runtime) {
		if err := mgr.Apply(rc); err != nil {
			log.Printf("watcher reload: %v", err)
		}
	})

	// Background IP geolocation resolver powers the world map. It runs
	// off the same SQLite store, resolving un-located IPs lazily and
	// caching the (coarse) result. The enabled flag is part of the
	// runtime config so it can be toggled from the settings panel.
	geoResolver := geo.New(store, geo.Options{
		Endpoint: cfg.Geo.Endpoint,
		Enabled:  rcStore.Get().GeoEnabled,
	})
	rcStore.Subscribe(func(rc runtimeconfig.Runtime) {
		geoResolver.SetEnabled(rc.GeoEnabled)
	})
	go geoResolver.Run(ctx)

	// Setup HTTP server
	mux := http.NewServeMux()

	// Auth service + bootstrap admin (Phase 2). Uses the same SQLite
	// database as the request log; tables live in independent
	// namespaces so the schemas don't collide.
	authSvc, err := auth.New(store.DB(), auth.Options{
		BcryptCost:   cfg.Auth.BcryptCost,
		SessionTTL:   time.Duration(cfg.Auth.SessionTTLHours) * time.Hour,
		CookieSecure: cfg.Auth.CookieSecure,
	})
	if err != nil {
		log.Fatalf("Failed to init auth: %v", err)
	}
	if ba := cfg.Auth.BootstrapAdmin; ba != nil && ba.Username != "" && ba.Password != "" {
		created, err := authSvc.BootstrapAdmin(ba.Username, ba.Password)
		if err != nil {
			log.Fatalf("Failed to bootstrap admin: %v", err)
		}
		if created {
			log.Printf("Bootstrap admin user %q created", ba.Username)
		}
	} else if cfg.Auth.RequireAccount {
		// Account mode was requested (e.g. by editing the config file)
		// but no usable credentials were supplied. Rather than locking
		// the operator out, mint an admin account with a random
		// password and print it to the backend log so they can sign in
		// and then change it.
		if n, _ := authSvc.CountUsers(); n == 0 {
			username := "admin"
			if cfg.Auth.BootstrapAdmin != nil && cfg.Auth.BootstrapAdmin.Username != "" {
				username = cfg.Auth.BootstrapAdmin.Username
			}
			password, err := randomPassword()
			if err != nil {
				log.Fatalf("Failed to generate admin password: %v", err)
			}
			if _, err := authSvc.CreateUser(username, password, auth.RoleAdmin); err != nil {
				log.Fatalf("Failed to create admin account: %v", err)
			}
			// Print the freshly generated, not-yet-used credential to
			// the process's standard output rather than through the
			// logging subsystem, so it is shown to the operator once on
			// the console without being persisted into aggregated logs.
			fmt.Fprintln(os.Stdout, "================================================================")
			fmt.Fprintln(os.Stdout, "Account mode is enabled but no account was configured.")
			fmt.Fprintln(os.Stdout, "A random admin account has been generated:")
			fmt.Fprintf(os.Stdout, "    username: %s\n", username)
			fmt.Fprintf(os.Stdout, "    password: %s\n", password)
			fmt.Fprintln(os.Stdout, "Sign in with these credentials and change the password.")
			fmt.Fprintln(os.Stdout, "================================================================")
		}
	} else {
		// Operators who skip bootstrap_admin should know the server
		// is running in no-account mode: any visitor to the UI acts
		// as administrator until the first user is created.
		if n, _ := authSvc.CountUsers(); n == 0 {
			log.Printf("Running in no-account mode: no users exist, so every request is treated as administrator. Create the first user from the Users tab to require login.")
		}
	}
	authH := auth.NewHandler(authSvc)
	authH.RegisterRoutes(mux)

	// API routes (protected behind RequireAuth).
	handler := api.NewHandler(store)
	handler.RegisterRoutesWithMiddleware(mux, authH.RequireAuth)

	// Settings panel API (admin-only). Includes the one-click restart
	// endpoint used by the "Restart server" button in the UI.
	cfgHandler := api.NewConfigHandler(rcStore, cfg.ListenAddr, cfg.DBPath, cfg.AllowedLogRoots, authSvc)
	cfgHandler.RegisterRoutes(mux, authH.RequireAdmin)

	// Static files (embedded web GUI)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to load static files: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: securityHeaders(mux),
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		cancel()
		mgr.Stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Server starting on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func importLogFile(store *storage.Store, path string, keywords []string, p parser.Parser) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return 0, fmt.Errorf("gzip reader: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var batch []*parser.LogEntry
	count := 0
	const batchSize = 1000

	for scanner.Scan() {
		entry, err := p.Parse(scanner.Text())
		if err != nil {
			continue
		}
		batch = append(batch, entry)
		if len(batch) >= batchSize {
			if err := store.InsertBatch(batch, keywords); err != nil {
				return count, err
			}
			count += len(batch)
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		if err := store.InsertBatch(batch, keywords); err != nil {
			return count, err
		}
		count += len(batch)
	}

	return count, scanner.Err()
}

// securityHeaders wraps the whole HTTP surface with defence-in-depth
// response headers. The dashboard is a single self-contained page (one
// inline script/style, no external assets), so the CSP allows inline
// code but pins every other source to the same origin — an injected
// payload cannot load external scripts or exfiltrate via fetch/XHR to
// another host, and the UI cannot be framed for clickjacking.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; "+
				"form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// randomPassword returns a URL-safe random password used when account
// mode is enabled without a configured admin password. The bytes come
// from crypto/rand so the credential is unpredictable.
func randomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
