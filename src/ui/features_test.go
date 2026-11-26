package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// The optional views are independent: each switch registers only its own
// routes, and with both off the UI is the one upstream ships.

func TestVersionWithAllViewsOffIsUpstreamResponse(t *testing.T) {
	server := &Server{version: "1.2.3", features: Features{}}
	w := httptest.NewRecorder()
	server.getVersion(w, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))

	// Byte for byte what upstream's getVersion encodes, trailing newline included.
	if got, want := w.Body.String(), "{\"version\":\"1.2.3\"}\n"; got != want {
		t.Errorf("version body = %q, want %q", got, want)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestVersionAdvertisesBothViewsWhenBothEnabled(t *testing.T) {
	server := &Server{version: "1.2.3", features: Features{JobOverview: true, SecurityView: true}}
	w := httptest.NewRecorder()
	server.getVersion(w, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))

	want := `{"version":"1.2.3","features":{"jobOverview":true,"securityView":true}}`
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Errorf("version body = %s, want %s", got, want)
	}
}

func TestEachViewRegistersOnlyItsOwnRoutes(t *testing.T) {
	tests := []struct {
		name     string
		features Features
		want     map[string]bool
	}{
		{name: "both off", features: Features{}, want: map[string]bool{"overview": false, "security": false}},
		{name: "overview only", features: Features{JobOverview: true}, want: map[string]bool{"overview": true, "security": false}},
		{name: "security only", features: Features{SecurityView: true}, want: map[string]bool{"overview": false, "security": true}},
		{name: "both on", features: Features{JobOverview: true, SecurityView: true}, want: map[string]bool{"overview": true, "security": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := overviewServer(nil, nil, tt.features)
			server.registerApiV1Routes(server.Router)
			server.registerUiRoutes(server.Router)

			found := map[string]int{}
			err := server.Router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
				path, pathErr := route.GetPathTemplate()
				if pathErr != nil {
					return nil
				}
				for view := range tt.want {
					if path == "/"+view || path == "/api/v1/"+view {
						found[view]++
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("failed to walk routes: %v", err)
			}
			for view, enabled := range tt.want {
				wantCount := 0
				if enabled {
					wantCount = 2 // page and API route
				}
				if found[view] != wantCount {
					t.Errorf("%s: %d routes registered, want %d", view, found[view], wantCount)
				}
			}
		})
	}
}
