package api

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSplitFilterValues(t *testing.T) {
	tests := []struct {
		name     string
		in       []string
		wantPos  string
		wantExcl []string
	}{
		{"empty", nil, "", nil},
		{"positive only", []string{"/api"}, "/api", nil},
		{"single exclude", []string{"!/health"}, "", []string{"/health"}},
		{"comma separated excludes", []string{"!/health, !/metrics"}, "", []string{"/health", "/metrics"}},
		{"mixed", []string{"/api, !/health"}, "/api", []string{"/health"}},
		{"repeated param", []string{"!/health", "!/metrics"}, "", []string{"/health", "/metrics"}},
		{"bare bang ignored", []string{"!"}, "", nil},
		{"whitespace trimmed", []string{" ! /health "}, "", []string{"/health"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, excl := splitFilterValues(tt.in)
			if pos != tt.wantPos {
				t.Errorf("positive = %q, want %q", pos, tt.wantPos)
			}
			if !reflect.DeepEqual(excl, tt.wantExcl) {
				t.Errorf("excludes = %v, want %v", excl, tt.wantExcl)
			}
		})
	}
}

func TestParseFilterExcludes(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/stats?path=!%2Fhealth,!%2Fmetrics&status=!404&ip=10.0&browser=!Bot", nil)
	f := parseFilter(r)
	if f.Path != "" || !reflect.DeepEqual(f.ExcludePath, []string{"/health", "/metrics"}) {
		t.Errorf("path: pos=%q excl=%v", f.Path, f.ExcludePath)
	}
	if f.Status != 0 || !reflect.DeepEqual(f.ExcludeStatus, []int{404}) {
		t.Errorf("status: pos=%d excl=%v", f.Status, f.ExcludeStatus)
	}
	if f.IP != "10.0" || f.ExcludeIP != nil {
		t.Errorf("ip: pos=%q excl=%v", f.IP, f.ExcludeIP)
	}
	if !reflect.DeepEqual(f.ExcludeBrowser, []string{"Bot"}) {
		t.Errorf("browser excl=%v", f.ExcludeBrowser)
	}
}
