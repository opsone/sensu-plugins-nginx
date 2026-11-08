package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev2 "github.com/sensu/core/v2"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// Config represents the check plugin config.
type Config struct {
	sensu.PluginConfig
	URL               string
	ActiveConnections int
	WaitingRequests   int
	Timeout           int
}

var (
	plugin = Config{
		PluginConfig: sensu.PluginConfig{
			Name:     "check-nginx-status",
			Short:    "Check nginx through its stub_status page",
			Keyspace: "sensu.io/plugins/check-nginx-status/config",
		},
	}

	options = []sensu.ConfigOption{
		&sensu.PluginConfigOption[string]{
			Path:      "url",
			Env:       "CHECK_NGINX_URL",
			Argument:  "url",
			Shorthand: "u",
			Usage:     "URL of the stub_status page, e.g. http://127.0.0.1/nginx-status (required)",
			Value:     &plugin.URL,
		},
		&sensu.PluginConfigOption[int]{
			Path:      "active-connections",
			Env:       "CHECK_NGINX_ACTIVE_CONNECTIONS",
			Argument:  "active-connections",
			Shorthand: "a",
			Default:   300,
			Usage:     "Warn when active connections exceed this value",
			Value:     &plugin.ActiveConnections,
		},
		&sensu.PluginConfigOption[int]{
			Path:      "waiting-requests",
			Env:       "CHECK_NGINX_WAITING_REQUESTS",
			Argument:  "waiting-requests",
			Shorthand: "w",
			Default:   30,
			Usage:     "Warn when waiting (idle keep-alive) connections exceed this value",
			Value:     &plugin.WaitingRequests,
		},
		&sensu.PluginConfigOption[int]{
			Path:     "timeout",
			Env:      "CHECK_NGINX_TIMEOUT",
			Argument: "timeout",
			Default:  10,
			Usage:    "HTTP request timeout in seconds",
			Value:    &plugin.Timeout,
		},
	}
)

func main() {
	check := sensu.NewGoCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *corev2.Event) (int, error) {
	if plugin.URL == "" {
		return sensu.CheckStateUnknown, fmt.Errorf("--url is required")
	}
	if err := validateURL(plugin.URL); err != nil {
		return sensu.CheckStateUnknown, err
	}
	if plugin.ActiveConnections < 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--active-connections must not be negative")
	}
	if plugin.WaitingRequests < 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--waiting-requests must not be negative")
	}
	if plugin.Timeout <= 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--timeout must be greater than 0")
	}
	return sensu.CheckStateOK, nil
}

func executeCheck(event *corev2.Event) (int, error) {
	client := &http.Client{
		Timeout: time.Duration(plugin.Timeout) * time.Second,
		// A redirect means a misconfigured status location: report it instead of following it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	var result Result
	resp, err := client.Get(plugin.URL)
	if err != nil {
		result = Result{sensu.CheckStateCritical, fmt.Sprintf("nginx is down: %v", err)}
	} else {
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			result = Result{sensu.CheckStateCritical, fmt.Sprintf("reading %s: %v", plugin.URL, err)}
		} else {
			result = evaluate(resp.StatusCode, resp.Header.Get("Location"), string(body),
				Thresholds{Active: plugin.ActiveConnections, Waiting: plugin.WaitingRequests})
		}
	}

	fmt.Printf("%s %s: %s\n", plugin.Name, stateLabel(result.State), result.Message)
	return result.State, nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid --url %q: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid --url %q: expected http(s)://host[:port]/path", raw)
	}
	return nil
}

// Status holds the counters of the nginx stub_status page.
type Status struct {
	Active, Reading, Writing, Waiting int
}

var (
	activeRe = regexp.MustCompile(`(?m)^Active connections:\s+(\d+)`)
	queuesRe = regexp.MustCompile(`(?m)^Reading:\s+(\d+)\s+Writing:\s+(\d+)\s+Waiting:\s+(\d+)`)
)

// parseStubStatus extracts the counters of a stub_status page.
func parseStubStatus(body string) (Status, error) {
	active := activeRe.FindStringSubmatch(body)
	queues := queuesRe.FindStringSubmatch(body)
	if active == nil || queues == nil {
		return Status{}, fmt.Errorf("not a stub_status page")
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return Status{Active: n(active[1]), Reading: n(queues[1]), Writing: n(queues[2]), Waiting: n(queues[3])}, nil
}

// Thresholds are the warning limits, exceeded when strictly greater.
type Thresholds struct {
	Active, Waiting int
}

// Result is the outcome of the check.
type Result struct {
	State   int
	Message string
}

// evaluate turns the HTTP response of the status page into a check result.
func evaluate(code int, location, body string, t Thresholds) Result {
	switch {
	case code >= 300 && code < 400:
		msg := fmt.Sprintf("status endpoint is misconfigured: HTTP %d", code)
		if location != "" {
			msg += " to " + location
		}
		return Result{sensu.CheckStateWarning, msg}
	case code != http.StatusOK:
		return Result{sensu.CheckStateCritical, fmt.Sprintf("nginx returned HTTP %d", code)}
	}

	s, err := parseStubStatus(body)
	if err != nil {
		return Result{sensu.CheckStateWarning, "status endpoint is misconfigured: " + err.Error()}
	}

	metrics := fmt.Sprintf("active %d, reading %d, writing %d, waiting %d", s.Active, s.Reading, s.Writing, s.Waiting)
	var over []string
	if s.Active > t.Active {
		over = append(over, fmt.Sprintf("active connections %d > %d", s.Active, t.Active))
	}
	if s.Waiting > t.Waiting {
		over = append(over, fmt.Sprintf("waiting requests %d > %d", s.Waiting, t.Waiting))
	}
	if len(over) > 0 {
		return Result{sensu.CheckStateWarning, strings.Join(over, ", ") + " (" + metrics + ")"}
	}
	return Result{sensu.CheckStateOK, metrics}
}

func stateLabel(state int) string {
	switch state {
	case sensu.CheckStateOK:
		return "OK"
	case sensu.CheckStateWarning:
		return "WARNING"
	case sensu.CheckStateCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
