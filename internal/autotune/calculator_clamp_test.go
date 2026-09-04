package autotune

import (
	"fmt"
	"strings"
	"testing"
)

// mockCalculatorStrict is mockCalculator with strict mode on.
func mockCalculatorStrict(profile Profile, memoryMB, cpus int) *Calculator {
	c := mockCalculator(profile, memoryMB, cpus)
	c.strict = true

	return c
}

// TestClampBootsSmallContainers is the regression for issue #133: a container too
// small for its profile must BOOT (clamped, with a warning) rather than exit
// PID 1. The limits are from the issue's table, where cbox-init 3.0.0 and 3.1.1
// crash-looped.
func TestClampBootsSmallContainers(t *testing.T) {
	cases := []struct {
		profile  Profile
		memoryMB int
		cpus     int
	}{
		{ProfileDev, 192, 1},
		{ProfileDev, 256, 1},
		{ProfileLight, 256, 1},
		{ProfileLight, 320, 1},
		{ProfileMedium, 384, 1},
		{ProfileMedium, 448, 1},
		{ProfileHeavy, 1024, 2},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%dMB", tc.profile, tc.memoryMB), func(t *testing.T) {
			cfg, err := mockCalculator(tc.profile, tc.memoryMB, tc.cpus).Calculate()
			if err != nil {
				t.Fatalf("non-strict must clamp and boot, got error: %v", err)
			}
			if cfg.MaxChildren < 1 {
				t.Errorf("expected at least 1 worker, got %d", cfg.MaxChildren)
			}
			if len(cfg.Warnings) == 0 {
				t.Errorf("expected a clamp/over-commit warning, got none")
			}
		})
	}
}

// TestStrictModeRefusesAMisfit: with strict on, a container too small for the
// profile is a hard error whose message names the profile and the smallest limit
// that boots it (issue #133, AC2 and AC4).
func TestStrictModeRefusesAMisfit(t *testing.T) {
	_, err := mockCalculatorStrict(ProfileHeavy, 256, 1).Calculate()
	if err == nil {
		t.Fatal("strict mode should refuse a container too small for the profile")
	}

	pc, _ := ProfileHeavy.GetConfig()
	if !strings.Contains(err.Error(), pc.Name) {
		t.Errorf("error should name the profile %q: %v", pc.Name, err)
	}
	minLimit := smallestBootableLimit(pc)
	if !strings.Contains(err.Error(), fmt.Sprintf("%dMB", minLimit)) {
		t.Errorf("error should name the smallest bootable limit %dMB: %v", minLimit, err)
	}
}

// TestStrictModeBootsWhenItFits: strict is not a blanket refusal — a container
// that fits the profile sizes normally.
func TestStrictModeBootsWhenItFits(t *testing.T) {
	cfg, err := mockCalculatorStrict(ProfileMedium, 2048, 4).Calculate()
	if err != nil {
		t.Fatalf("strict mode should size a fitting container without error: %v", err)
	}
	if cfg.MaxChildren < 1 {
		t.Errorf("expected at least 1 worker, got %d", cfg.MaxChildren)
	}
}

// TestSmallestBootableLimitIsConsistent: the reported minimum is exactly the
// boundary the sizer uses — at it the profile's minimum fits, one MB below it the
// sizer clamps. This is what keeps the pre-check number and the sizing from
// disagreeing (issue #133, point 2).
func TestSmallestBootableLimitIsConsistent(t *testing.T) {
	for profile := range Profiles {
		pc, _ := profile.GetConfig()
		minLimit := smallestBootableLimit(pc)

		// A generous CPU budget so CPU never caps below the memory floor.
		at, err := sizeWorkers(minLimit, 64, pc.MaxMemoryUsage, pc, false)
		if err != nil {
			t.Fatalf("%s: sizeWorkers at the reported minimum errored: %v", pc.Name, err)
		}
		if at.maxChildren < pc.MinWorkers {
			t.Errorf("%s: minimum %dMB yields %d workers, below the profile minimum %d",
				pc.Name, minLimit, at.maxChildren, pc.MinWorkers)
		}

		below, _ := sizeWorkers(minLimit-1, 64, pc.MaxMemoryUsage, pc, false)
		if below.maxChildren >= pc.MinWorkers {
			t.Errorf("%s: %dMB (one below the reported minimum) already meets the floor %d; the minimum is overstated",
				pc.Name, minLimit-1, pc.MinWorkers)
		}
	}
}
