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
	URL                string
	ActiveConnections  int
	WritingConnections int
	WaitingConnections int
	LoadGrace          int
	StateDir           string
	Timeout            int
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
			Usage:     "Warn when active connections (reading, writing and waiting) exceed this value (0: disabled)",
			Value:     &plugin.ActiveConnections,
		},
		&sensu.PluginConfigOption[int]{
			Path:      "writing-connections",
			Env:       "CHECK_NGINX_WRITING_CONNECTIONS",
			Argument:  "writing-connections",
			Shorthand: "W",
			Default:   200,
			Usage:     "Warn when writing connections (requests being processed) exceed this value (0: disabled)",
			Value:     &plugin.WritingConnections,
		},
		&sensu.PluginConfigOption[int]{
			Path:      "waiting-connections",
			Env:       "CHECK_NGINX_WAITING_CONNECTIONS",
			Argument:  "waiting-connections",
			Shorthand: "w",
			Default:   0,
			Usage:     "Warn when waiting (idle keep-alive) connections exceed this value (0: disabled)",
			Value:     &plugin.WaitingConnections,
		},
		&sensu.PluginConfigOption[int]{
			Path:     "load-grace",
			Env:      "CHECK_NGINX_LOAD_GRACE",
			Argument: "load-grace",
			Default:  180,
			Usage:    "Seconds a counter may stay above its threshold before alerting (0: immediately)",
			Value:    &plugin.LoadGrace,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "state-dir",
			Env:      "CHECK_NGINX_STATE_DIR",
			Argument: "state-dir",
			Default:  "/var/cache/sensu/sensu-agent",
			Usage:    "Directory of the file remembering the thresholds exceeded under the grace delay (writable by the agent user)",
			Value:    &plugin.StateDir,
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
	for name, v := range map[string]int{
		"active-connections": plugin.ActiveConnections, "writing-connections": plugin.WritingConnections,
		"waiting-connections": plugin.WaitingConnections, "load-grace": plugin.LoadGrace,
	} {
		if v < 0 {
			return sensu.CheckStateUnknown, fmt.Errorf("--%s must not be negative", name)
		}
	}
	if plugin.LoadGrace > 0 && plugin.StateDir == "" {
		return sensu.CheckStateUnknown, fmt.Errorf("--state-dir is required with --load-grace")
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
			th := Thresholds{
				Active:  plugin.ActiveConnections,
				Writing: plugin.WritingConnections,
				Waiting: plugin.WaitingConnections,
				Grace:   time.Duration(plugin.LoadGrace) * time.Second,
			}
			now := time.Now().Truncate(time.Second) // the state keeps whole seconds
			var tr *Tracker
			if th.Grace > 0 {
				tr = LoadTracker(plugin.StateDir, now)
			}
			code, location := resp.StatusCode, resp.Header.Get("Location")
			result = evaluate(code, location, string(body), th, now, tr)
			if tr.Save() != nil {
				result = evaluate(code, location, string(body), th, now, tr) // grace state not kept: no grace
			}
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

// Thresholds are the warning limits, exceeded when strictly greater; 0 disables a limit. A limit
// exceeded is only reported once it has lasted Grace.
type Thresholds struct {
	Active, Writing, Waiting int
	Grace                    time.Duration
}

// Result is the outcome of the check.
type Result struct {
	State   int
	Message string
}

// evaluate turns the HTTP response of the status page into a check result. The tracker remembers
// since when each limit has been exceeded; without it (nil, or unusable state), a limit exceeded is
// reported at once.
func evaluate(code int, location, body string, t Thresholds, now time.Time, tr *Tracker) Result {
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
	var over, within []string
	for _, c := range []struct {
		key, label   string
		value, limit int
	}{
		{"active", "active connections", s.Active, t.Active},
		{"writing", "writing connections", s.Writing, t.Writing},
		{"waiting", "waiting connections", s.Waiting, t.Waiting},
	} {
		if c.limit == 0 || c.value <= c.limit {
			continue
		}
		text := fmt.Sprintf("%s %d > %d", c.label, c.value, c.limit)
		var since time.Time
		if t.Grace > 0 {
			since = tr.Since(c.key)
		}
		switch d := now.Sub(since).Round(time.Second); {
		case since.IsZero():
			over = append(over, text)
		case d < t.Grace:
			within = append(within, fmt.Sprintf("%s for %s, within grace", text, d))
		default:
			over = append(over, fmt.Sprintf("%s for %s", text, d))
		}
	}

	if len(within) > 0 {
		metrics += "; " + strings.Join(within, ", ")
	}
	result := Result{sensu.CheckStateOK, metrics}
	if len(over) > 0 {
		result = Result{sensu.CheckStateWarning, strings.Join(over, ", ") + " (" + metrics + ")"}
	}
	if err := tr.Err(); err != nil {
		result.State = sensu.CheckStateWarning
		result.Message += fmt.Sprintf("\ngrace WARNING: %v (no grace)", err)
	}
	return result
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
