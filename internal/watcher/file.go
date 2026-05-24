package watcher

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/moehoshio/web-request-attribution/internal/parser"
	"github.com/moehoshio/web-request-attribution/internal/storage"
)

// FileWatcher monitors a log file using fsnotify for efficient event-driven watching.
type FileWatcher struct {
	store          *storage.Store
	logPath        string
	keywords       []string
	parser         parser.Parser
	readCompressed bool
}

// NewFileWatcher creates a new fsnotify-based file watcher.
//
// If p is nil, ParseLine auto-detection is used. When readCompressed is true,
// any `.gz` siblings of logPath are imported once on startup before tailing the
// live file.
func NewFileWatcher(store *storage.Store, logPath string, keywords []string, p parser.Parser, readCompressed bool) *FileWatcher {
	if p == nil {
		p, _ = parser.New(parser.FormatConfig{Engine: "auto"})
	}
	return &FileWatcher{
		store:          store,
		logPath:        logPath,
		keywords:       keywords,
		parser:         p,
		readCompressed: readCompressed,
	}
}

// Watch starts watching the log file for new entries using fsnotify.
// It handles log rotation by detecting file truncation or recreation.
func (fw *FileWatcher) Watch(ctx context.Context) error {
	// Import any rotated `.gz` archives first if requested.
	if fw.readCompressed {
		if err := fw.importCompressedSiblings(ctx); err != nil {
			log.Printf("File watcher compressed import error: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := fw.watchLoop(ctx); err != nil {
			log.Printf("File watcher error: %v, retrying in 5s...", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// importCompressedSiblings reads `.gz` files in the same directory whose name
// starts with the live log's base name (e.g. `access.log.1.gz`).
func (fw *FileWatcher) importCompressedSiblings(ctx context.Context) error {
	dir := filepath.Dir(fw.logPath)
	base := filepath.Base(fw.logPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == base || !strings.HasPrefix(name, base) || !strings.HasSuffix(name, ".gz") {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		full := filepath.Join(dir, name)
		n, err := fw.importGzip(full)
		if err != nil {
			log.Printf("Import gzip %s: %v", full, err)
			continue
		}
		log.Printf("Imported %d records from %s", n, full)
	}
	return nil
}

func (fw *FileWatcher) importGzip(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	return fw.consumeReader(gz)
}

// consumeReader reads lines from r and inserts parsed entries in batches.
func (fw *FileWatcher) consumeReader(r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var batch []*parser.LogEntry
	count := 0
	const batchSize = 1000
	for scanner.Scan() {
		entry, err := fw.parser.Parse(scanner.Text())
		if err != nil {
			continue
		}
		batch = append(batch, entry)
		if len(batch) >= batchSize {
			if err := fw.store.InsertBatch(batch, fw.keywords); err != nil {
				return count, err
			}
			count += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := fw.store.InsertBatch(batch, fw.keywords); err != nil {
			return count, err
		}
		count += len(batch)
	}
	return count, scanner.Err()
}

func (fw *FileWatcher) watchLoop(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// Watch the directory to detect file recreation (log rotation)
	dir := filepath.Dir(fw.logPath)
	if err := watcher.Add(dir); err != nil {
		return err
	}

	f, err := os.Open(fw.logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end - only process new entries
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	baseName := filepath.Base(fw.logPath)

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			eventBase := filepath.Base(event.Name)

			// Handle log rotation: file was removed or renamed, then recreated
			if eventBase == baseName && (event.Has(fsnotify.Create)) {
				// File was recreated (log rotation), reopen it
				f.Close()
				time.Sleep(100 * time.Millisecond) // brief wait for file to be ready
				f, err = os.Open(fw.logPath)
				if err != nil {
					return err
				}
				defer f.Close()
				offset = 0
				scanner = bufio.NewScanner(f)
				scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
				continue
			}

			if eventBase == baseName && event.Has(fsnotify.Write) {
				// Check for truncation (log rotation via copytruncate)
				info, err := f.Stat()
				if err != nil {
					return err
				}
				if info.Size() < offset {
					// File was truncated, seek to beginning
					if _, err := f.Seek(0, io.SeekStart); err != nil {
						return err
					}
					offset = 0
					scanner = bufio.NewScanner(f)
					scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
				}

				// Read new lines
				var batch []*parser.LogEntry
				for scanner.Scan() {
					entry, err := fw.parser.Parse(scanner.Text())
					if err != nil {
						continue
					}
					batch = append(batch, entry)
				}
				if scanner.Err() != nil {
					return scanner.Err()
				}

				if len(batch) > 0 {
					if err := fw.store.InsertBatch(batch, fw.keywords); err != nil {
						log.Printf("File watcher insert error: %v", err)
					}
				}

				// Update offset
				newOffset, err := f.Seek(0, io.SeekCurrent)
				if err != nil {
					return err
				}
				offset = newOffset
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("File watcher fsnotify error: %v", err)
		}
	}
}
