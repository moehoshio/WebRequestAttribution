package main

import (
	"bufio"
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/moehoshio/nginx-request-attribution/internal/api"
	"github.com/moehoshio/nginx-request-attribution/internal/config"
	"github.com/moehoshio/nginx-request-attribution/internal/parser"
	"github.com/moehoshio/nginx-request-attribution/internal/storage"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	importFile := flag.String("import", "", "import a log file and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.Close()

	// If import mode, process file and exit
	if *importFile != "" {
		count, err := importLogFile(store, *importFile, cfg.Keywords)
		if err != nil {
			log.Fatalf("Import failed: %v", err)
		}
		fmt.Printf("Imported %d records from %s\n", count, *importFile)
		return
	}

	// Start log watcher in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Watch {
		go watchLog(ctx, store, cfg.LogPath, cfg.Keywords)
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// API routes
	handler := api.NewHandler(store)
	handler.RegisterRoutes(mux)

	// Static files (embedded web GUI)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to load static files: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Server starting on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func importLogFile(store *storage.Store, path string, keywords []string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var batch []*parser.LogEntry
	count := 0
	batchSize := 1000

	for scanner.Scan() {
		entry, err := parser.ParseLine(scanner.Text())
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

func watchLog(ctx context.Context, store *storage.Store, logPath string, keywords []string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := tailFile(ctx, store, logPath, keywords); err != nil {
			log.Printf("Log watch error: %v, retrying in 5s...", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func tailFile(ctx context.Context, store *storage.Store, logPath string, keywords []string) error {
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for scanner.Scan() {
				entry, err := parser.ParseLine(scanner.Text())
				if err != nil {
					continue
				}
				if err := store.Insert(entry, keywords); err != nil {
					log.Printf("Insert error: %v", err)
				}
			}
			if scanner.Err() != nil {
				return scanner.Err()
			}
		}
	}
}
