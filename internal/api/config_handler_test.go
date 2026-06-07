package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func decodeBrowse(t *testing.T, h *ConfigHandler, path string) (int, browseResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/browse?path="+path, nil)
	rec := httptest.NewRecorder()
	h.handleBrowse(rec, req)
	var resp browseResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp
}

func TestHandleBrowseListsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "access.log"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	h := &ConfigHandler{allowedLogRoots: []string{root}}

	// With a single configured root and no path, browsing should
	// descend straight into that root.
	code, resp := decodeBrowse(t, h, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if resp.AtRoots {
		t.Fatalf("single root should not present a roots screen")
	}
	var names []string
	for _, e := range resp.Entries {
		names = append(names, e.Name)
	}
	wantDirFirst := false
	for _, e := range resp.Entries {
		if e.IsDir && e.Name == "sub" {
			wantDirFirst = true
		}
		if e.Name == ".hidden" {
			t.Fatalf("dotfiles should be hidden, got %v", names)
		}
	}
	if !wantDirFirst {
		t.Fatalf("expected sub directory in listing, got %v", names)
	}
}

func TestHandleBrowseRejectsEscape(t *testing.T) {
	root := t.TempDir()
	h := &ConfigHandler{allowedLogRoots: []string{root}}

	code, _ := decodeBrowse(t, h, filepath.Dir(root))
	if code != http.StatusForbidden {
		t.Fatalf("expected 403 for path outside roots, got %d", code)
	}
}

func TestHandleBrowseMultipleRoots(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	h := &ConfigHandler{allowedLogRoots: []string{a, b}}

	code, resp := decodeBrowse(t, h, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !resp.AtRoots || len(resp.Entries) != 2 {
		t.Fatalf("expected a 2-entry roots screen, got at_roots=%v entries=%d", resp.AtRoots, len(resp.Entries))
	}
}
