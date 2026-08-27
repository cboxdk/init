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

// TestSchedulerRegistersTheTimezoneAwareSchedule is the test the one above
// should have been.
//
// TestScheduledJob_HonorsTimezone asserts on job.schedule — the parsed,
// CRON_TZ-bound schedule that NewScheduledJobWithOptions builds. But the
// Scheduler registered the RAW expression with cron.AddJob, which re-parsed it
// in the cron instance's own default location, so job.schedule was dead code and
// schedule_timezone had no effect at all. The test was green against a field
// nothing used. This one goes through the Scheduler and reads back the entry the
// cron library will actually fire.
func TestSchedulerRegistersTheTimezoneAwareSchedule(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// 1 March 2026 is inside EST (UTC-5), so 03:00 New York is 08:00 UTC.
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		timezone string
		wantUTC  int
	}{
		{"UTC", 3},
		{"America/New_York", 8},
	} {
		t.Run(c.timezone, func(t *testing.T) {
			s := NewScheduler(nil, 10, lg)
			if err := s.AddJob("nightly", "0 3 * * *", c.timezone); err != nil {
				t.Fatalf("AddJob: %v", err)
			}

			job, ok := s.GetJob("nightly")
			if !ok {
				t.Fatal("job not registered")
			}

			entries := s.cron.Entries()
			if len(entries) != 1 {
				t.Fatalf("expected 1 cron entry, got %d", len(entries))
			}

			// This is the schedule the cron library will actually use.
			next := entries[0].Schedule.Next(base).UTC()
			t.Logf("timezone %s -> cron entry fires at %s", c.timezone, next.Format(time.RFC3339))

			if next.Hour() != c.wantUTC {
				t.Errorf("the registered cron entry fires at %02d:00 UTC, want %02d:00; "+
					"schedule_timezone %q never reached the scheduler",
					next.Hour(), c.wantUTC, c.timezone)
			}

			// And the entry agrees with the job's own parsed schedule.
			if want := job.CronSchedule().Next(base).UTC(); !next.Equal(want) {
				t.Errorf("cron entry (%s) and job schedule (%s) disagree", next, want)
			}
		})
	}
}
