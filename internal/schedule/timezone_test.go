package schedule

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// The cron spec was parsed with the parser's default location (time.Local), so
// schedule_timezone was stored and then ignored: a job configured with the
// documented default "UTC" fired at the container's local time instead. Any
// image that sets TZ ran every nightly job at the wrong hour.
func TestScheduledJob_HonorsTimezone(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// 1 March 2026 is inside EST (UTC-5), so 03:00 New York is 08:00 UTC.
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		timezone string
		wantUTC  int
	}{
		{"UTC", 3},
		{"America/New_York", 8},
	}
	for _, c := range cases {
		t.Run(c.timezone, func(t *testing.T) {
			j, err := NewScheduledJobWithOptions("nightly", "0 3 * * *", c.timezone, 10, nil, lg, JobOptions{})
			if err != nil {
				t.Fatalf("build job: %v", err)
			}
			next := j.schedule.Next(base).UTC()
			t.Logf("timezone %s -> next run %s", c.timezone, next.Format(time.RFC3339))
			if next.Hour() != c.wantUTC {
				t.Errorf("with schedule_timezone %q the job fires at %02d:00 UTC, want %02d:00",
					c.timezone, next.Hour(), c.wantUTC)
			}
		})
	}
}

// An unresolvable timezone must be rejected when the job is built, not silently
// fall back to local time.
func TestScheduledJob_RejectsUnknownTimezone(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if _, err := NewScheduledJobWithOptions("j", "0 3 * * *", "Mars/Olympus_Mons", 10, nil, lg, JobOptions{}); err == nil {
		t.Error("an unknown timezone must be rejected")
	}
}
