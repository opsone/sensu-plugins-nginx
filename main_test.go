package main

import (
	"strings"
	"testing"
	"time"

	"github.com/sensu/sensu-plugin-sdk/sensu"
)

const stubStatus = `Active connections: 291
server accepts handled requests
 16630948 16630948 31070465
Reading: 6 Writing: 179 Waiting: 106
`

func TestParseStubStatus(t *testing.T) {
	got, err := parseStubStatus(strings.ReplaceAll(stubStatus, "\n", "\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Status{Active: 291, Reading: 6, Writing: 179, Waiting: 106}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	for _, body := range []string{"", "<html>Welcome to nginx!</html>", "Active connections: 1\n"} {
		if _, err := parseStubStatus(body); err == nil {
			t.Errorf("parseStubStatus(%q): expected an error", body)
		}
	}
}

var now = time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)

func TestEvaluate(t *testing.T) {
	defaults := Thresholds{Active: 300, Writing: 200}
	tests := []struct {
		name     string
		code     int
		location string
		body     string
		t        Thresholds
		state    int
		contains string
	}{
		{"healthy", 200, "", stubStatus, defaults, sensu.CheckStateOK, "active 291, reading 6, writing 179, waiting 106"},
		{"threshold equal is ok", 200, "", stubStatus, Thresholds{Active: 291, Writing: 179, Waiting: 106}, sensu.CheckStateOK, "active 291"},
		{"all disabled", 200, "", stubStatus, Thresholds{}, sensu.CheckStateOK, "active 291"},
		{"active over", 200, "", stubStatus, Thresholds{Active: 100}, sensu.CheckStateWarning, "active connections 291 > 100 ("},
		{"writing over", 200, "", stubStatus, Thresholds{Writing: 100}, sensu.CheckStateWarning, "writing connections 179 > 100 ("},
		{"waiting over", 200, "", stubStatus, Thresholds{Waiting: 30}, sensu.CheckStateWarning, "waiting connections 106 > 30 ("},
		{"all over", 200, "", stubStatus, Thresholds{Active: 1, Writing: 1, Waiting: 1}, sensu.CheckStateWarning, "active connections 291 > 1, writing connections 179 > 1, waiting connections 106 > 1"},
		{"not stub_status", 200, "", "<html></html>", defaults, sensu.CheckStateWarning, "not a stub_status page"},
		{"301", 301, "https://example.com/nginx-status", "", defaults, sensu.CheckStateWarning, "HTTP 301 to https://example.com/nginx-status"},
		{"302", 302, "", "", defaults, sensu.CheckStateWarning, "misconfigured: HTTP 302"},
		{"404", 404, "", "", defaults, sensu.CheckStateCritical, "HTTP 404"},
		{"502", 502, "", stubStatus, defaults, sensu.CheckStateCritical, "HTTP 502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluate(tt.code, tt.location, tt.body, tt.t, now, nil)
			if got.State != tt.state || !strings.Contains(got.Message, tt.contains) {
				t.Errorf("got %d %q, want %d containing %q", got.State, got.Message, tt.state, tt.contains)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1/nginx-status", "https://[::1]:8443/status"} {
		if err := validateURL(u); err != nil {
			t.Errorf("validateURL(%q): %v", u, err)
		}
	}
	for _, u := range []string{"127.0.0.1/nginx-status", "ftp://host/x", "http:///path", "http://%zz"} {
		if err := validateURL(u); err == nil {
			t.Errorf("validateURL(%q): expected an error", u)
		}
	}
}
