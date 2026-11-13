package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// graceTh: writing (179) and waiting (106) over their limits, active (291) under.
var graceTh = Thresholds{Active: 300, Writing: 100, Waiting: 30, Grace: 3 * time.Minute}

// tracker returns an in-memory tracker: conditions first seen ago.
func tracker(ago map[string]time.Duration) *Tracker {
	t := &Tracker{now: now, seen: map[string]int64{}, next: map[string]int64{}}
	for k, d := range ago {
		t.seen[k] = now.Add(-d).Unix()
	}
	return t
}

func TestEvaluateGrace(t *testing.T) {
	tests := []struct {
		name    string
		ago     map[string]time.Duration
		noState bool
		state   int
		want    string
	}{
		{name: "new peak", state: sensu.CheckStateOK,
			want: "check: active 291, reading 6, writing 179, waiting 106; writing connections 179 > 100 for 0s, within grace, waiting connections 106 > 30 for 0s, within grace"},
		{name: "one past grace", ago: map[string]time.Duration{"writing": 3 * time.Minute, "waiting": time.Minute}, state: sensu.CheckStateWarning,
			want: "check: writing connections 179 > 100 for 3m0s (active 291, reading 6, writing 179, waiting 106; waiting connections 106 > 30 for 1m0s, within grace)"},
		{name: "both past grace", ago: map[string]time.Duration{"writing": 4 * time.Minute, "waiting": 5 * time.Minute}, state: sensu.CheckStateWarning,
			want: "check: writing connections 179 > 100 for 4m0s, waiting connections 106 > 30 for 5m0s ("},
		{name: "no state", noState: true, state: sensu.CheckStateWarning,
			want: "check: writing connections 179 > 100, waiting connections 106 > 30 ("},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := tracker(tt.ago)
			if tt.noState {
				tr.err = errors.New("no state")
			}
			got := evaluate(200, "", stubStatus, graceTh, now, tr)
			if got.State != tt.state || !strings.Contains("check: "+got.Message, tt.want) {
				t.Errorf("evaluate() = %d %q, want %d with %q", got.State, got.Message, tt.state, tt.want)
			}
			if tt.noState != strings.Contains(got.Message, "grace WARNING: no state (no grace)") {
				t.Errorf("state error reporting: %q", got.Message)
			}
		})
	}

	// A limit no longer exceeded is forgotten: the next peak starts a new delay.
	tr := tracker(map[string]time.Duration{"active": 10 * time.Minute})
	evaluate(200, "", stubStatus, graceTh, now, tr)
	if _, ok := tr.next["active"]; ok {
		t.Error("active kept although under its limit")
	}

	// Without grace, no state is read nor needed.
	noGrace := graceTh
	noGrace.Grace = 0
	tr = tracker(nil)
	tr.err = errors.New("no state")
	if got := evaluate(200, "", stubStatus, noGrace, now, tr); got.State != sensu.CheckStateWarning || strings.Contains(got.Message, "grace") {
		t.Errorf("no grace: %d %q", got.State, got.Message)
	}
}

func TestTracker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, graceFile)

	// Nothing seen, no state: nothing written.
	tr := LoadTracker(dir, now)
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state written without condition: %v", err)
	}

	// First seen: now; the next runs keep it.
	tr = LoadTracker(dir, now)
	if got := tr.Since("writing"); !got.Equal(now) {
		t.Errorf("Since = %v, want %v", got, now)
	}
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	tr = LoadTracker(dir, now.Add(time.Minute))
	if got := tr.Since("writing"); !got.Equal(now) {
		t.Errorf("Since next run = %v, want %v", got, now)
	}
	tr.Save()

	// Condition gone for one run: dropped; seen again: new time.
	LoadTracker(dir, now.Add(2*time.Minute)).Save()
	tr = LoadTracker(dir, now.Add(3*time.Minute))
	if got := tr.Since("writing"); !got.Equal(now.Add(3 * time.Minute)) {
		t.Errorf("Since after recovery = %v", got)
	}

	// Corrupted state: start over.
	os.WriteFile(path, []byte("garbage"), 0o644)
	tr = LoadTracker(dir, now)
	if got := tr.Since("writing"); !got.Equal(now) || tr.Err() != nil {
		t.Errorf("corrupted state: Since %v, err %v", got, tr.Err())
	}

	var none *Tracker
	if !none.Since("x").IsZero() || none.Err() != nil || none.Save() != nil {
		t.Error("nil tracker")
	}
}

func TestTrackerUnwritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if tr := LoadTracker(dir, now); tr.Save() != nil || tr.Err() != nil {
		t.Errorf("error without condition")
	}

	// Within grace at first, then evaluated again without grace once the state cannot be kept.
	tr := LoadTracker(dir, now)
	if got := evaluate(200, "", stubStatus, graceTh, now, tr); got.State != sensu.CheckStateOK {
		t.Errorf("first evaluate() = %d %q", got.State, got.Message)
	}
	if tr.Save() == nil {
		t.Fatal("Save() succeeded in a missing directory")
	}
	got := evaluate(200, "", stubStatus, graceTh, now, tr)
	if got.State != sensu.CheckStateWarning ||
		!strings.HasPrefix(got.Message, "writing connections 179 > 100, waiting connections 106 > 30 (") ||
		!strings.Contains(got.Message, "grace WARNING: writing "+filepath.Join(dir, graceFile)) {
		t.Errorf("evaluate() = %d %q", got.State, got.Message)
	}
}
