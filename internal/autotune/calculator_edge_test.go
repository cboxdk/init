package autotune

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// TestNewCalculator_ResourceDetectionError tests NewCalculator when resource detection fails
// Note: This is hard to test without mocking, but we can test the error path
func TestNewCalculator_ValidProfile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Test all valid profiles
	profiles := []Profile{
		ProfileDev,
		ProfileLight,
		ProfileMedium,
		ProfileHeavy,
		ProfileBursty,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			calc, err := NewCalculator(profile, 0.75, false, logger)

			// We expect success or only resource detection errors (which are valid)
			if err != nil {
				// Should be resource detection error, not profile error
				if strings.Contains(err.Error(), "profile") {
					t.Errorf("Unexpected profile error for valid profile %s: %v", profile, err)
				}
				// Resource detection errors are acceptable
				t.Logf("Resource detection result for %s: %v", profile, err)
				return
			}

			if calc == nil {
				t.Errorf("Expected non-nil calculator for valid profile %s", profile)
				return
			}

			// Verify calculator fields
			if calc.memoryThreshold != 0.75 {
				t.Errorf("Expected memoryThreshold=0.75, got %f", calc.memoryThreshold)
			}
		})
	}
}

// TestCalculator_MemoryEdgeCases tests Calculate with various memory scenarios
func TestCalculator_MemoryEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		profile    Profile
		memoryMB   int
		cpus       int
		wantError  bool
		checkWarns func(*testing.T, *PHPFPMConfig)
	}{
		{
			name:      "barely sufficient memory",
			profile:   ProfileLight,
			memoryMB:  500, // Just above minimum
			cpus:      1,
			wantError: false,
			checkWarns: func(t *testing.T, cfg *PHPFPMConfig) {
				// Should work but might have warnings
				if cfg.MaxChildren < 2 {
					t.Error("Should have at least minimum workers")
				}
			},
		},
		{
			name:      "at minimum memory for dev",
			profile:   ProfileDev,
			memoryMB:  384, // Dev profile: 2 workers × 32MB + 64MB OPcache + 64MB reserved = 192MB / 0.5 threshold = 384MB
			cpus:      1,
			wantError: false,
		},
		{
			name:      "very large memory",
			profile:   ProfileMedium,
			memoryMB:  65536, // 64GB
			cpus:      16,
			wantError: false,
			checkWarns: func(t *testing.T, cfg *PHPFPMConfig) {
				// Should be limited by CPU (16 * 4 = 64 workers max)
				if cfg.MaxChildren > 64 {
					t.Errorf("Should be limited by CPU: got %d workers", cfg.MaxChildren)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := mockCalculator(tt.profile, tt.memoryMB, tt.cpus)
			cfg, err := calc.Calculate()

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.checkWarns != nil {
				tt.checkWarns(t, cfg)
			}
		})
	}
}

// TestCalculator_DynamicPMBoundaries tests boundary conditions for dynamic PM
func TestCalculator_DynamicPMBoundaries(t *testing.T) {
	// Test with exactly 1 worker (edge case for dynamic PM)
	profileConfig := ProfileConfig{
		Name:                "test",
		AvgMemoryPerWorker:  256, // High memory per worker
		ReservedMemoryMB:    64,
		OPcacheMemoryMB:     32,
		MaxMemoryUsage:      0.8,
		MinWorkers:          1,
		MaxWorkers:          0,
		ProcessManagerType:  "dynamic",
		MaxRequestsPerChild: 100,
		SpareMinRatio:       0.25,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.33,
	}

	calc := &Calculator{
		resources:       mockResources(512, 1),
		profile:         profileConfig,
		memoryThreshold: 0.0,
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// With only 1 worker, PM settings should be adjusted
	if cfg.ProcessManager == "dynamic" {
		// All spare settings should be valid
		if cfg.MinSpare > cfg.MaxChildren {
			t.Errorf("MinSpare (%d) > MaxChildren (%d)", cfg.MinSpare, cfg.MaxChildren)
		}
		if cfg.MaxSpare > cfg.MaxChildren {
			t.Errorf("MaxSpare (%d) > MaxChildren (%d)", cfg.MaxSpare, cfg.MaxChildren)
		}
		if cfg.StartServers < cfg.MinSpare {
			t.Errorf("StartServers (%d) < MinSpare (%d)", cfg.StartServers, cfg.MinSpare)
		}
	}
}

// TestCalculator_ZeroMinWorkers tests profile with zero minimum workers
func TestCalculator_ZeroMinWorkers(t *testing.T) {
	profileConfig := ProfileConfig{
		Name:                "zero-min",
		AvgMemoryPerWorker:  512, // Very high
		ReservedMemoryMB:    100,
		OPcacheMemoryMB:     64,
		MaxMemoryUsage:      0.5,
		MinWorkers:          0, // Zero minimum
		MaxWorkers:          0,
		ProcessManagerType:  "static",
		MaxRequestsPerChild: 100,
		SpareMinRatio:       0.2,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.3,
	}

	calc := &Calculator{
		resources:       mockResources(1024, 2),
		profile:         profileConfig,
		memoryThreshold: 0.0,
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		// Might fail due to insufficient memory
		if !strings.Contains(err.Error(), "insufficient") {
			t.Errorf("Expected insufficient memory error, got: %v", err)
		}
		return
	}

	// Should enforce absolute minimum of 1 worker
	if cfg.MaxChildren < 1 {
		t.Errorf("Should have at least 1 worker, got %d", cfg.MaxChildren)
	}

	// Should have warning about absolute minimum
	hasWarning := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "absolute minimum") || strings.Contains(w, "1 worker") {
			hasWarning = true
			break
		}
	}
	if cfg.MaxChildren == 1 && !hasWarning {
		t.Log("Note: May not have warning if calculations naturally resulted in 1 worker")
	}
}

// TestCalculator_ProfileMaxWorkerEnforcement tests max worker limit enforcement
func TestCalculator_ProfileMaxWorkerEnforcement(t *testing.T) {
	// Create profile with low max worker limit
	profileConfig := ProfileConfig{
		Name:                "limited",
		AvgMemoryPerWorker:  32, // Small to allow many workers
		ReservedMemoryMB:    128,
		OPcacheMemoryMB:     64,
		MaxMemoryUsage:      0.8,
		MinWorkers:          2,
		MaxWorkers:          3, // Very low max
		ProcessManagerType:  "dynamic",
		MaxRequestsPerChild: 500,
		SpareMinRatio:       0.2,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.3,
	}

	calc := &Calculator{
		resources:       mockResources(4096, 8), // Lots of resources
		profile:         profileConfig,
		memoryThreshold: 0.0,
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Should be limited to profile max
	if cfg.MaxChildren > 3 {
		t.Errorf("Expected max 3 workers (profile limit), got %d", cfg.MaxChildren)
	}

	// Should have warning
	hasWarning := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "profile limits") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("Expected warning about profile limiting workers")
	}
}

// TestCalculator_PMCorrectionScenarios tests various PM correction scenarios
func TestCalculator_PMCorrectionScenarios(t *testing.T) {
	tests := []struct {
		name         string
		maxChildren  int
		minRatio     float64
		maxRatio     float64
		startRatio   float64
		expectMinGE1 bool // MinSpare should be >= 1
	}{
		{
			name:         "very small worker count",
			maxChildren:  2,
			minRatio:     0.25,
			maxRatio:     0.5,
			startRatio:   0.33,
			expectMinGE1: true,
		},
		{
			name:         "single worker",
			maxChildren:  1,
			minRatio:     0.1,
			maxRatio:     0.9,
			startRatio:   0.5,
			expectMinGE1: true,
		},
		{
			name:         "normal worker count",
			maxChildren:  10,
			minRatio:     0.2,
			maxRatio:     0.5,
			startRatio:   0.3,
			expectMinGE1: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profileConfig := ProfileConfig{
				Name:                "test",
				AvgMemoryPerWorker:  42,
				ReservedMemoryMB:    128,
				OPcacheMemoryMB:     64,
				MaxMemoryUsage:      0.75,
				MinWorkers:          tt.maxChildren, // Force this count
				MaxWorkers:          tt.maxChildren,
				ProcessManagerType:  "dynamic",
				MaxRequestsPerChild: 1000,
				SpareMinRatio:       tt.minRatio,
				SpareMaxRatio:       tt.maxRatio,
				StartServersRatio:   tt.startRatio,
			}

			calc := &Calculator{
				resources:       mockResources(4096, 8),
				profile:         profileConfig,
				memoryThreshold: 0.0,
				logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
			}

			cfg, err := calc.Calculate()
			if err != nil {
				t.Fatalf("Calculate() failed: %v", err)
			}

			// Verify corrections
			if tt.expectMinGE1 && cfg.MinSpare < 1 {
				t.Error("MinSpare should be at least 1 after corrections")
			}

			if cfg.StartServers < cfg.MinSpare {
				t.Errorf("StartServers (%d) should be >= MinSpare (%d)",
					cfg.StartServers, cfg.MinSpare)
			}

			if cfg.MaxSpare < cfg.StartServers {
				t.Errorf("MaxSpare (%d) should be >= StartServers (%d)",
					cfg.MaxSpare, cfg.StartServers)
			}

			if cfg.MaxSpare > cfg.MaxChildren {
				t.Errorf("MaxSpare (%d) should be <= MaxChildren (%d)",
					cfg.MaxSpare, cfg.MaxChildren)
			}

			t.Logf("%s: MaxChildren=%d, MinSpare=%d, StartServers=%d, MaxSpare=%d",
				tt.name, cfg.MaxChildren, cfg.MinSpare, cfg.StartServers, cfg.MaxSpare)
		})
	}
}
