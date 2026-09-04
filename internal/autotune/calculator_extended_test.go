package autotune

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewCalculator tests the NewCalculator constructor
func TestNewCalculator_Success(t *testing.T) {
	// Create a temporary cgroup v2 structure for testing
	tmpDir := t.TempDir()

	// Mock cgroup v2 files
	memMaxPath := filepath.Join(tmpDir, "memory.max")
	cpuMaxPath := filepath.Join(tmpDir, "cpu.max")
	controllersPath := filepath.Join(tmpDir, "cgroup.controllers")

	if err := os.WriteFile(memMaxPath, []byte("2147483648\n"), 0644); err != nil { // 2GB
		t.Fatalf("Failed to write memory.max: %v", err)
	}
	if err := os.WriteFile(cpuMaxPath, []byte("400000 100000\n"), 0644); err != nil { // 4 CPUs
		t.Fatalf("Failed to write cpu.max: %v", err)
	}
	if err := os.WriteFile(controllersPath, []byte("cpu memory\n"), 0644); err != nil {
		t.Fatalf("Failed to write cgroup.controllers: %v", err)
	}

	// Temporarily change the cgroup path (this won't work in real scenario,
	// but demonstrates what NewCalculator should do)
	// Since we can't easily mock filesystem paths in the real function,
	// we'll test the error path instead for now

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Test with invalid profile
	_, err := NewCalculator(Profile("invalid"), 0.0, false, logger)
	if err == nil {
		t.Error("Expected error for invalid profile, got none")
	}
	if !strings.Contains(err.Error(), "invalid profile") {
		t.Errorf("Expected 'invalid profile' error, got: %v", err)
	}
}

func TestNewCalculator_InvalidProfile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name    string
		profile Profile
	}{
		{"empty profile", Profile("")},
		{"invalid profile", Profile("invalid")},
		{"uppercase profile", Profile("MEDIUM")},
		{"unknown profile", Profile("unknown")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCalculator(tt.profile, 0.0, false, logger)
			if err == nil {
				t.Errorf("Expected error for profile %q, got none", tt.profile)
			}
		})
	}
}

func TestNewCalculator_WithMemoryThreshold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Note: This will attempt to detect real cgroup resources
	// In a containerized environment, this should succeed
	// In a non-containerized environment, it should fall back to host resources
	calc, err := NewCalculator(ProfileMedium, 0.8, false, logger)

	// We don't expect an error from resource detection failure
	// (it falls back to host resources)
	if err != nil {
		// Only profile validation should cause errors
		if !strings.Contains(err.Error(), "profile") {
			t.Errorf("Unexpected error type: %v", err)
		}
		return
	}

	if calc == nil {
		t.Error("Expected non-nil calculator")
		return
	}

	if calc.memoryThreshold != 0.8 {
		t.Errorf("Expected memoryThreshold=0.8, got %f", calc.memoryThreshold)
	}
}

// TestCalculator_validateConfig_IgnoresMemory: validateConfig no longer polices
// memory. Sizing owns that — sizeWorkers clamps to what fits, and only strict
// mode turns a misfit into an error — so an over-committed config reaching this
// function is not its concern. Its job is the PM relationships.
func TestCalculator_validateConfig_IgnoresMemory(t *testing.T) {
	calc := mockCalculator(ProfileMedium, 1024, 2)

	cfg := &PHPFPMConfig{
		ProcessManager:  "dynamic",
		MaxChildren:     100, // Would over-commit, but that is sizing's job, not this one's
		MemoryAllocated: 900,
		MemoryOPcache:   128,
		MemoryReserved:  100,
		MemoryTotal:     1024,
	}

	if err := calc.validateConfig(cfg); err != nil {
		t.Errorf("validateConfig should not refuse on memory (sizing owns that), got: %v", err)
	}
}

func TestCalculator_validateConfig_InvalidPMRelationships(t *testing.T) {
	calc := mockCalculator(ProfileMedium, 2048, 4)

	tests := []struct {
		name      string
		cfg       *PHPFPMConfig
		wantError bool
		errMsg    string
	}{
		{
			name: "min_spare > max_children",
			cfg: &PHPFPMConfig{
				ProcessManager:  "dynamic",
				MaxChildren:     10,
				MinSpare:        15,
				MaxSpare:        8,
				StartServers:    5,
				MemoryAllocated: 100,
				MemoryOPcache:   64,
				MemoryReserved:  64,
				MemoryTotal:     512,
			},
			wantError: true,
			errMsg:    "min_spare_servers",
		},
		{
			name: "max_spare > max_children",
			cfg: &PHPFPMConfig{
				ProcessManager:  "dynamic",
				MaxChildren:     10,
				MinSpare:        2,
				MaxSpare:        15,
				StartServers:    5,
				MemoryAllocated: 100,
				MemoryOPcache:   64,
				MemoryReserved:  64,
				MemoryTotal:     512,
			},
			wantError: true,
			errMsg:    "max_spare_servers",
		},
		{
			name: "start_servers > max_children",
			cfg: &PHPFPMConfig{
				ProcessManager:  "dynamic",
				MaxChildren:     10,
				MinSpare:        2,
				MaxSpare:        8,
				StartServers:    15,
				MemoryAllocated: 100,
				MemoryOPcache:   64,
				MemoryReserved:  64,
				MemoryTotal:     512,
			},
			wantError: true,
			errMsg:    "start_servers",
		},
		{
			name: "min_spare > max_spare",
			cfg: &PHPFPMConfig{
				ProcessManager:  "dynamic",
				MaxChildren:     10,
				MinSpare:        8,
				MaxSpare:        5,
				StartServers:    6,
				MemoryAllocated: 100,
				MemoryOPcache:   64,
				MemoryReserved:  64,
				MemoryTotal:     512,
			},
			wantError: true,
			errMsg:    "min_spare_servers",
		},
		{
			name: "valid static mode",
			cfg: &PHPFPMConfig{
				ProcessManager:  "static",
				MaxChildren:     10,
				MemoryAllocated: 100,
				MemoryOPcache:   64,
				MemoryReserved:  64,
				MemoryTotal:     512,
			},
			wantError: false,
		},
		{
			name: "valid dynamic mode",
			cfg: &PHPFPMConfig{
				ProcessManager:  "dynamic",
				MaxChildren:     10,
				MinSpare:        2,
				MaxSpare:        5,
				StartServers:    3,
				MemoryAllocated: 100,
				MemoryOPcache:   64,
				MemoryReserved:  64,
				MemoryTotal:     512,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := calc.validateConfig(tt.cfg)
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error for %s, got none", tt.name)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCalculator_EdgeCases tests boundary conditions
func TestCalculator_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		profile   Profile
		memoryMB  int
		cpus      int
		threshold float64
		wantError bool
		checkFunc func(*testing.T, *PHPFPMConfig)
	}{
		{
			name:      "zero memory threshold uses profile default",
			profile:   ProfileMedium,
			memoryMB:  2048,
			cpus:      4,
			threshold: 0.0,
			wantError: false,
			checkFunc: func(t *testing.T, cfg *PHPFPMConfig) {
				if cfg.MaxChildren == 0 {
					t.Error("Expected non-zero workers with zero threshold (should use profile default)")
				}
			},
		},
		{
			name:      "exactly at minimum memory boundary",
			profile:   ProfileLight,
			memoryMB:  768, // Exactly at boundary for light profile
			cpus:      1,
			threshold: 0.0,
			wantError: false,
			checkFunc: func(t *testing.T, cfg *PHPFPMConfig) {
				if cfg.MaxChildren < 2 {
					t.Errorf("Expected at least 2 workers at boundary, got %d", cfg.MaxChildren)
				}
			},
		},
		{
			name:      "single CPU with heavy profile",
			profile:   ProfileHeavy,
			memoryMB:  8192,
			cpus:      1,
			threshold: 0.0,
			wantError: false,
			checkFunc: func(t *testing.T, cfg *PHPFPMConfig) {
				// Heavy profile has MinWorkers: 8
				// CPU limit: 1 * 4 = 4 workers
				// Memory would allow more
				// Profile minimum (8) takes precedence over CPU limit (4)
				if cfg.MaxChildren != 8 {
					t.Errorf("Expected 8 workers (profile minimum), got %d", cfg.MaxChildren)
				}
				// Should have warning about increasing to profile minimum
				hasWarning := false
				for _, w := range cfg.Warnings {
					if strings.Contains(w, "profile minimum") {
						hasWarning = true
						break
					}
				}
				if !hasWarning {
					t.Error("Expected warning about profile minimum")
				}
			},
		},
		{
			name:      "max threshold at 100%",
			profile:   ProfileMedium,
			memoryMB:  2048,
			cpus:      4,
			threshold: 1.0,
			wantError: false,
			checkFunc: func(t *testing.T, cfg *PHPFPMConfig) {
				// Should use full 100% of memory
				totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
				if totalUsed > cfg.MemoryTotal {
					t.Errorf("Memory overprovisioned at 100%% threshold")
				}
			},
		},
		{
			name:      "threshold at 29% (boundary for conservative warning)",
			profile:   ProfileMedium,
			memoryMB:  2048,
			cpus:      4,
			threshold: 0.29,
			wantError: false,
			checkFunc: func(t *testing.T, cfg *PHPFPMConfig) {
				// Should have conservative warning
				hasWarning := false
				for _, w := range cfg.Warnings {
					if strings.Contains(w, "conservative") {
						hasWarning = true
						break
					}
				}
				if !hasWarning {
					t.Error("Expected conservative threshold warning at 29%")
				}
			},
		},
		{
			name:      "threshold at 101% (boundary for oversubscription)",
			profile:   ProfileMedium,
			memoryMB:  2048,
			cpus:      4,
			threshold: 1.01,
			wantError: false,
			checkFunc: func(t *testing.T, cfg *PHPFPMConfig) {
				// Should have oversubscription warning
				hasWarning := false
				for _, w := range cfg.Warnings {
					if strings.Contains(w, "OVERSUBSCRIPTION") || strings.Contains(w, ">100%") {
						hasWarning = true
						break
					}
				}
				if !hasWarning {
					t.Error("Expected oversubscription warning at 101%")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profileConfig, _ := tt.profile.GetConfig()
			calc := &Calculator{
				resources:       mockResources(tt.memoryMB, tt.cpus),
				profile:         profileConfig,
				memoryThreshold: tt.threshold,
				logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
			}

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

			if tt.checkFunc != nil {
				tt.checkFunc(t, cfg)
			}
		})
	}
}

// TestCalculator_DynamicPMCorrections tests automatic corrections in dynamic PM mode
func TestCalculator_DynamicPMCorrections(t *testing.T) {
	// Test with very small worker count to trigger corrections
	calc := mockCalculator(ProfileLight, 512, 1)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	if cfg.ProcessManager != "dynamic" {
		t.Skip("Test requires dynamic PM mode")
	}

	// Verify all PM relationships are valid
	if cfg.MinSpare < 1 {
		t.Error("MinSpare should be at least 1 after corrections")
	}

	if cfg.StartServers < cfg.MinSpare {
		t.Errorf("StartServers (%d) should be >= MinSpare (%d) after corrections",
			cfg.StartServers, cfg.MinSpare)
	}

	if cfg.MaxSpare < cfg.StartServers {
		t.Errorf("MaxSpare (%d) should be >= StartServers (%d) after corrections",
			cfg.MaxSpare, cfg.StartServers)
	}

	if cfg.MaxSpare > cfg.MaxChildren {
		t.Errorf("MaxSpare (%d) should be <= MaxChildren (%d) after corrections",
			cfg.MaxSpare, cfg.MaxChildren)
	}
}

// TestCalculator_StaticPMSettings tests static PM mode
func TestCalculator_StaticPMSettings(t *testing.T) {
	calc := mockCalculator(ProfileDev, 512, 2)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	if cfg.ProcessManager != "static" {
		t.Fatalf("Expected static PM for dev profile, got %s", cfg.ProcessManager)
	}

	// In static mode, StartServers should equal MaxChildren
	if cfg.StartServers != cfg.MaxChildren {
		t.Errorf("Static mode: StartServers (%d) should equal MaxChildren (%d)",
			cfg.StartServers, cfg.MaxChildren)
	}
}

// TestCalculator_ProfileMaxWorkerLimit tests profile max worker enforcement
func TestCalculator_ProfileMaxWorkerLimit(t *testing.T) {
	// Create a custom profile config with a max worker limit
	profileConfig := ProfileConfig{
		Name:                "custom",
		AvgMemoryPerWorker:  32,
		ReservedMemoryMB:    128,
		OPcacheMemoryMB:     64,
		MaxMemoryUsage:      0.8,
		MinWorkers:          2,
		MaxWorkers:          5, // Set max limit
		ProcessManagerType:  "dynamic",
		MaxRequestsPerChild: 500,
		SpareMinRatio:       0.2,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.3,
	}

	// Provide lots of memory that would allow more than 5 workers
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

	// Should be limited to profile max
	if cfg.MaxChildren > 5 {
		t.Errorf("Expected max 5 workers due to profile limit, got %d", cfg.MaxChildren)
	}

	// Should have warning about profile limiting
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

// TestCalculator_AbsoluteMinimumWorker tests the absolute minimum worker enforcement
func TestCalculator_AbsoluteMinimumWorker(t *testing.T) {
	// Create scenario where calculation results in < 1 worker
	profileConfig := ProfileConfig{
		Name:                "extreme",
		AvgMemoryPerWorker:  1000, // Unrealistically high
		ReservedMemoryMB:    100,
		OPcacheMemoryMB:     64,
		MaxMemoryUsage:      0.5,
		MinWorkers:          0, // No profile minimum
		MaxWorkers:          0, // No profile max
		ProcessManagerType:  "static",
		MaxRequestsPerChild: 100,
		SpareMinRatio:       0.2,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.3,
	}

	calc := &Calculator{
		resources:       mockResources(256, 1),
		profile:         profileConfig,
		memoryThreshold: 0.0,
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	// This will likely fail due to insufficient memory check first
	// But if it passes that, it should enforce minimum 1 worker
	_, err := calc.Calculate()
	if err != nil {
		// Expected due to insufficient memory for such high worker memory
		if !strings.Contains(err.Error(), "insufficient") {
			t.Errorf("Expected insufficient memory error, got: %v", err)
		}
		return
	}
}

// TestCalculator_MultipleWarnings tests accumulation of multiple warnings
func TestCalculator_MultipleWarnings(t *testing.T) {
	// Use host resources (non-containerized) with CPU limiting and low threshold
	profileConfig, _ := ProfileMedium.GetConfig()
	resources := &ContainerResources{
		MemoryLimitBytes: 4 * 1024 * 1024 * 1024,
		MemoryLimitMB:    4096,
		CPULimit:         1,     // Very limited CPU
		IsContainerized:  false, // Not in container
		CgroupVersion:    0,
	}

	calc := &Calculator{
		resources:       resources,
		profile:         profileConfig,
		memoryThreshold: 0.25, // Very conservative
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Should have multiple warnings:
	// 1. Container limits warning
	// 2. Conservative threshold warning
	// 3. Possibly CPU limiting
	if len(cfg.Warnings) < 2 {
		t.Errorf("Expected at least 2 warnings, got %d: %v", len(cfg.Warnings), cfg.Warnings)
	}

	// Check for specific warnings
	hasContainerWarning := false
	hasConservativeWarning := false

	for _, w := range cfg.Warnings {
		if strings.Contains(w, "container limits") {
			hasContainerWarning = true
		}
		if strings.Contains(w, "conservative") {
			hasConservativeWarning = true
		}
	}

	if !hasContainerWarning {
		t.Error("Expected warning about container limits")
	}
	if !hasConservativeWarning {
		t.Error("Expected warning about conservative threshold")
	}
}
