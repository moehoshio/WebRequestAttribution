package runtimeconfig

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"
	"github.com/moehoshio/WebRequestAttribution/internal/parser"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedRuntime() Runtime {
	return Runtime{
		Watch:    true,
		Keywords: []string{"login", "admin"},
		Sources: []Source{{
			Name: "nginx",
			Type: SourceFile,
			Path: "/var/log/nginx/access.log",
			Format: parser.FormatConfig{
				Engine: "nginx",
				Preset: "combined",
			},
		}},
	}
}

func TestNewSeedsFirstLaunch(t *testing.T) {
	db := newTestDB(t)
	seed := seedRuntime()
	s, err := New(db, seed)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := s.Get()
	if !got.Watch || len(got.Sources) != 1 || got.Sources[0].Path != "/var/log/nginx/access.log" {
		t.Fatalf("seed not applied: %+v", got)
	}

	// Re-open: should NOT re-seed.
	s2, err := New(db, Runtime{Watch: false})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !s2.Get().Watch {
		t.Fatalf("seed was re-applied on reopen")
	}
}

func TestSetNotifiesSubscribers(t *testing.T) {
	db := newTestDB(t)
	s, err := New(db, Runtime{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var calls int32
	var lastWatch atomic.Bool
	unsub := s.Subscribe(func(rc Runtime) {
		atomic.AddInt32(&calls, 1)
		lastWatch.Store(rc.Watch)
	})
	defer unsub()

	rc := seedRuntime()
	if err := s.Set(rc, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("subscriber not invoked: %d", atomic.LoadInt32(&calls))
	}
	if !lastWatch.Load() {
		t.Fatalf("subscriber saw wrong value")
	}

	// Unsubscribe stops calls.
	unsub()
	if err := s.Set(Runtime{Watch: false}, nil); err != nil {
		t.Fatalf("Set 2: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("subscriber still called after unsubscribe")
	}
}

func TestValidateRejectsBadSource(t *testing.T) {
	db := newTestDB(t)
	s, _ := New(db, Runtime{})

	cases := []struct {
		name string
		rc   Runtime
	}{
		{"missing type", Runtime{Watch: true, Sources: []Source{{Path: "/x"}}}},
		{"file no path", Runtime{Watch: true, Sources: []Source{{Type: SourceFile}}}},
		{"syslog no addr", Runtime{Watch: true, Sources: []Source{{Type: SourceSyslog}}}},
		{"bad proto", Runtime{Watch: true, Sources: []Source{{Type: SourceSyslog, Addr: ":1", Proto: "sctp"}}}},
		{"unknown type", Runtime{Watch: true, Sources: []Source{{Type: "udpish", Name: "x"}}}},
		{"bad keyword", Runtime{Keywords: []string{"a\nb"}}},
		{"bad custom format", Runtime{Sources: []Source{{
			Type: SourceFile, Path: "/x",
			Format: parser.FormatConfig{Engine: "custom"}, // missing pattern
		}}}},
	}
	for _, tc := range cases {
		if err := s.Set(tc.rc, nil); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestPathAllowedHonoursRoots(t *testing.T) {
	if !pathAllowed("/var/log/nginx/access.log", []string{"/var/log"}) {
		t.Error("expected /var/log/nginx/access.log to be allowed under /var/log")
	}
	if pathAllowed("/etc/passwd", []string{"/var/log"}) {
		t.Error("expected /etc/passwd to be rejected")
	}
	if pathAllowed("/var/log/../etc/passwd", []string{"/var/log"}) {
		t.Error("expected traversal to be rejected")
	}
	if !pathAllowed("/anything", nil) {
		t.Error("nil roots should allow everything")
	}
}

func TestSetRejectsDisallowedPath(t *testing.T) {
	db := newTestDB(t)
	s, _ := New(db, Runtime{})
	rc := Runtime{
		Watch: true,
		Sources: []Source{{
			Type: SourceFile,
			Path: "/etc/shadow",
			Format: parser.FormatConfig{Engine: "auto"},
		}},
	}
	if err := s.Set(rc, []string{"/var/log"}); err == nil {
		t.Fatal("expected disallowed path to be rejected")
	}
}

func TestSourceKey(t *testing.T) {
	a := Source{Type: SourceFile, Path: "/var/log/a"}
	b := Source{Type: SourceFile, Path: "/var/log/a"}
	if a.Key() != b.Key() {
		t.Fatal("identical file sources must have same key")
	}
	c := Source{Type: SourceSyslog, Addr: ":1514", Proto: "udp"}
	if c.Key() == a.Key() {
		t.Fatal("file and syslog must have different keys")
	}
}

func TestPathAllowedSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// A symlink inside the allowed root pointing outside it must not
	// grant access to the link target.
	link := filepath.Join(root, "evil")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if PathAllowed(link, []string{root}) {
		t.Fatal("symlink escaping the root should be rejected")
	}
	if PathAllowed(filepath.Join(link, "secret.log"), []string{root}) {
		t.Fatal("path under an escaping symlink should be rejected")
	}

	// Regular paths inside the root stay allowed, including files that
	// do not exist yet.
	if !PathAllowed(filepath.Join(root, "access.log"), []string{root}) {
		t.Fatal("non-existent file directly under the root should be allowed")
	}
	sub := filepath.Join(root, "nginx")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !PathAllowed(filepath.Join(sub, "access.log"), []string{root}) {
		t.Fatal("file in a subdirectory of the root should be allowed")
	}

	// Lexical traversal is still rejected.
	if PathAllowed(filepath.Join(root, "..", "etc", "passwd"), []string{root}) {
		t.Fatal("dot-dot traversal should be rejected")
	}
}
