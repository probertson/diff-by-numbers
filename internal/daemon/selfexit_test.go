package daemon

import (
	"testing"
	"time"
)

// shouldExit is the whole of the self-exit decision, kept pure so it can be
// tested without racing a real process or a timer.
func TestShouldExitOnlyWhenNoReviewIsActiveAndItIsIdlePastGrace(t *testing.T) {
	const grace = 60 * time.Second

	cases := []struct {
		name   string
		active bool
		idle   time.Duration
		want   bool
	}{
		{"an active review keeps it alive however long idle", true, 5 * time.Minute, false},
		{"idle within the grace window waits", false, 30 * time.Second, false},
		{"idle exactly at the grace boundary exits", false, 60 * time.Second, true},
		{"idle past the grace window exits", false, 90 * time.Second, true},
	}

	for _, c := range cases {
		if got := shouldExit(c.active, c.idle, grace); got != c.want {
			t.Errorf("%s: shouldExit(active=%v, idle=%v) = %v, want %v",
				c.name, c.active, c.idle, got, c.want)
		}
	}
}
