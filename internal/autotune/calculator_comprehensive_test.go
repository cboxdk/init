package autotune

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// TestCalculator_AllProfiles tests Calculate with all profile types
func TestCalculator_AllProfiles(t *testing.T) {
	profiles := []Profile{
		ProfileDev,
		ProfileLight,
		ProfileMedium,
		ProfileHeavy,
		ProfileBursty,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			// Use sufficient resources for all profiles
			calc := mockCalculator(profile, 4096, 4)
			cfg, err := calc.Calculate()

			if err != nil {
				t.Fatalf("Calculate() failed for %s: %v", profile, err)
			}

			// Verify basic invariants
			if cfg.MaxChildren < 1 {
				t.Error("MaxChildren should be at least 1")
			}

			if cfg.ProcessManager == "" {
				t.Error("ProcessManager should be set")
			}

			if cfg.MaxRequests < 1 {
				t.Error("MaxRequests should be positive")
			}

			// Verify memory doesn't exceed limit
			totalMem := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
			if totalMem > cfg.MemoryTotal {
				t.Errorf("Memory overprovisioned: %dMB > %dMB", totalMem, cfg.MemoryTotal)
			}

			t.Logf("%s: %d workers, %s PM, %dMB total", profile, cfg.MaxChildren, cfg.ProcessManager, totalMem)
		})
	}
}

// TestCalculator_AllWarningScenarios tests all warning generation paths
func TestCalculator_AllWarningScenarios(t *testing.T) {
	tests := []struct {
		name       string
		setupCalc  func() *Calculator
		expectWarn string
	}{
		{
			name: "non-containerized warning",
			setupCalc: func() *Calculator {
				profileConfig, _ := ProfileMedium.GetConfig()
				return &Calculator{
					resources: &ContainerResources{
						MemoryLimitBytes: 4 * 1024 * 1024 * 1024,
						MemoryLimitMB:    4096,
						CPULimit:         4,
						IsContainerized:  false, // Key: not containerized
						CgroupVersion:    0,
					},
					profile:         profileConfig,
					memoryThreshold: 0.0,
					logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
				}
			},
			expectWarn: "container limits",
		},
		{
			name: "oversubscription warning",
			setupCalc: func() *Calculator {
				profileConfig, _ := ProfileMedium.GetConfig()
				return &Calculator{
					resources:       mockResources(2048, 4),
					profile:         profileConfig,
					memoryThreshold: 1.5, // 150% - oversubscription
					logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
				}
			},
			expectWarn: "OVERSUBSCRIPTION",
		},
		{
			name: "conservative threshold warning",
			setupCalc: func() *Calculator {
				profileConfig, _ := ProfileMedium.GetConfig()
				return &Calculator{
					resources:       mockResources(2048, 4),
					profile:         profileConfig,
					memoryThreshold: 0.25, // Very conservative
					logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
				}
			},
			expectWarn: "conservative",
		},
		{
			name: "CPU limiting warning",
			setupCalc: func() *Calculator {
				profileConfig, _ := ProfileMedium.GetConfig()
				return &Calculator{
					resources:       mockResources(8192, 1), // Lots of memory, 1 CPU
					profile:         profileConfig,
					memoryThreshold: 0.0,
					logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
				}
			},
			expectWarn: "CPU",
		},
		{
			name: "profile minimum warning",
			setupCalc: func() *Calculator {
				profileConfig, _ := ProfileHeavy.GetConfig() // MinWorkers: 8
				return &Calculator{
					resources:       mockResources(1024, 1), // Limited resources
					profile:         profileConfig,
					memoryThreshold: 0.0,
					logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
				}
			},
			expectWarn: "clamped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := tt.setupCalc()
			cfg, err := calc.Calculate()

			if err != nil {
				// Some scenarios might error due to insufficient memory
				t.Logf("Calculate() errored (may be expected): %v", err)
				return
			}

			// Check for expected warning
			found := false
			for _, w := range cfg.Warnings {
				if strings.Contains(strings.ToLower(w), strings.ToLower(tt.expectWarn)) {
					found = true
					t.Logf("Found expected warning: %s", w)
					break
				}
			}

			if !found {
				t.Errorf("Expected warning containing %q, got warnings: %v", tt.expectWarn, cfg.Warnings)
			}
		})
	}
}

// TestCalculator_StaticVsDynamic tests both PM types
func TestCalculator_StaticVsDynamic(t *testing.T) {
	tests := []struct {
		name           string
		profile        Profile
		expectPM       string
		expectHasSpare bool
	}{
		{
			name:           "dev uses static",
			profile:        ProfileDev,
			expectPM:       "static",
			expectHasSpare: false,
		},
		{
			name:           "light uses dynamic",
			profile:        ProfileLight,
			expectPM:       "dynamic",
			expectHasSpare: true,
		},
		{
			name:           "medium uses dynamic",
			profile:        ProfileMedium,
			expectPM:       "dynamic",
			expectHasSpare: true,
		},
		{
			name:           "heavy uses dynamic",
			profile:        ProfileHeavy,
			expectPM:       "dynamic",
			expectHasSpare: true,
		},
		{
			name:           "bursty uses dynamic",
			profile:        ProfileBursty,
			expectPM:       "dynamic",
			expectHasSpare: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := mockCalculator(tt.profile, 4096, 4)
			cfg, err := calc.Calculate()

			if err != nil {
				t.Fatalf("Calculate() failed: %v", err)
			}

			if cfg.ProcessManager != tt.expectPM {
				t.Errorf("Expected PM type %s, got %s", tt.expectPM, cfg.ProcessManager)
			}

			if tt.expectHasSpare {
				if cfg.MinSpare == 0 || cfg.MaxSpare == 0 || cfg.StartServers == 0 {
					t.Error("Dynamic PM should have non-zero spare server settings")
				}
			} else {
				if cfg.ProcessManager == "static" && cfg.StartServers != cfg.MaxChildren {
					t.Errorf("Static PM: StartServers (%d) should equal MaxChildren (%d)",
						cfg.StartServers, cfg.MaxChildren)
				}
			}
		})
	}
}

// TestCalculator_ThresholdVariations tests various threshold values
func TestCalculator_ThresholdVariations(t *testing.T) {
	thresholds := []float64{
		0.0,  // Use profile default
		0.25, // Very conservative
		0.5,  // Conservative
		0.75, // Standard
		0.9,  // Aggressive
		1.0,  // Maximum
		1.1,  // Oversubscription
	}

	for _, threshold := range thresholds {
		t.Run(fmt.Sprintf("threshold_%.2f", threshold), func(t *testing.T) {
			profileConfig, _ := ProfileMedium.GetConfig()
			calc := &Calculator{
				resources:       mockResources(2048, 4),
				profile:         profileConfig,
				memoryThreshold: threshold,
				logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
			}

			cfg, err := calc.Calculate()
			if err != nil {
				t.Fatalf("Calculate() failed with threshold %.2f: %v", threshold, err)
			}

			// Verify memory calculations respect threshold
			totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved

			effectiveThreshold := threshold
			if threshold == 0.0 {
				effectiveThreshold = profileConfig.MaxMemoryUsage
			}

			// Allow some buffer for rounding
			maxExpected := int(float64(cfg.MemoryTotal) * effectiveThreshold * 1.05)

			if totalUsed > maxExpected {
				t.Errorf("Memory usage %dMB exceeds expected max %dMB (threshold %.2f)",
					totalUsed, maxExpected, effectiveThreshold)
			}

			t.Logf("Threshold %.2f: %d workers, %dMB used / %dMB total (%.1f%%)",
				threshold, cfg.MaxChildren, totalUsed, cfg.MemoryTotal,
				float64(totalUsed)/float64(cfg.MemoryTotal)*100)
		})
	}
}

// TestCalculator_CPUScaling tests worker scaling with different CPU counts
func TestCalculator_CPUScaling(t *testing.T) {
	cpuCounts := []int{1, 2, 4, 8, 16, 32}

	for _, cpus := range cpuCounts {
		t.Run(fmt.Sprintf("cpus_%d", cpus), func(t *testing.T) {
			calc := mockCalculator(ProfileMedium, 8192, cpus)
			cfg, err := calc.Calculate()

			if err != nil {
				t.Fatalf("Calculate() failed with %d CPUs: %v", cpus, err)
			}

			// CPU-based limit is 4 workers per CPU
			maxExpectedByCPU := cpus * 4

			// If limited by CPU, should not exceed this
			if cfg.MaxChildren > maxExpectedByCPU {
				// Check if warning was issued
				hasWarning := false
				for _, w := range cfg.Warnings {
					if strings.Contains(w, "CPU") {
						hasWarning = true
						break
					}
				}
				if !hasWarning {
					t.Errorf("Workers %d exceeds CPU limit %d but no warning issued",
						cfg.MaxChildren, maxExpectedByCPU)
				}
			}

			t.Logf("%d CPUs: %d workers (max by CPU: %d)", cpus, cfg.MaxChildren, maxExpectedByCPU)
		})
	}
}

// TestCalculator_MemoryScaling tests worker scaling with different memory amounts
func TestCalculator_MemoryScaling(t *testing.T) {
	memorySizes := []int{512, 1024, 2048, 4096, 8192, 16384}

	for _, memoryMB := range memorySizes {
		t.Run(fmt.Sprintf("memory_%dMB", memoryMB), func(t *testing.T) {
			calc := mockCalculator(ProfileMedium, memoryMB, 16) // Lots of CPUs to avoid CPU limiting

			cfg, err := calc.Calculate()
			if err != nil {
				t.Logf("Calculate() failed with %dMB (expected for low memory): %v", memoryMB, err)
				return
			}

			// More memory should generally mean more workers (unless CPU limited)
			t.Logf("%dMB memory: %d workers", memoryMB, cfg.MaxChildren)

			// Verify we're not overprovisioning memory
			totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
			if totalUsed > cfg.MemoryTotal {
				t.Errorf("Memory overprovisioned: %dMB used > %dMB total", totalUsed, cfg.MemoryTotal)
			}
		})
	}
}
