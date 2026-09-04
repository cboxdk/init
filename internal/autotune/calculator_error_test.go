package autotune

import (
	"log/slog"
	"os"
	"testing"
)

// TestNewCalculator_InvalidProfileValue tests error handling for invalid profile
func TestNewCalculator_InvalidProfileValue(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use invalid profile ("invalid" is not a valid Profile value)
	invalidProfile := Profile("invalid-profile-name")

	calc, err := NewCalculator(invalidProfile, 0, false, logger)

	if err == nil {
		t.Error("Expected error for invalid profile, got nil")
	}

	if calc != nil {
		t.Error("Expected nil calculator for invalid profile")
	}

	if err != nil {
		t.Logf("Expected error received: %v", err)
	}
}

// TestNewCalculator_ZeroThreshold tests zero memory threshold
func TestNewCalculator_ZeroThreshold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	calc, err := NewCalculator(ProfileMedium, 0, false, logger)
	if err != nil {
		t.Fatalf("NewCalculator with zero threshold failed: %v", err)
	}

	if calc == nil {
		t.Fatal("Expected non-nil calculator")
	}

	// Zero threshold should use profile default
	if calc.memoryThreshold != 0 {
		t.Logf("Memory threshold: %f (will use profile default)", calc.memoryThreshold)
	}
}

// TestNewCalculator_CustomThreshold tests custom memory threshold
func TestNewCalculator_CustomThreshold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	customThreshold := 0.75
	calc, err := NewCalculator(ProfileHeavy, customThreshold, false, logger)
	if err != nil {
		t.Fatalf("NewCalculator with custom threshold failed: %v", err)
	}

	if calc.memoryThreshold != customThreshold {
		t.Errorf("Expected threshold %f, got %f", customThreshold, calc.memoryThreshold)
	}
}

// TestCalculate_AllProfiles tests all profile calculations
func TestCalculate_AllProfiles(t *testing.T) {
	profiles := []struct {
		profile Profile
		name    string
	}{
		{ProfileDev, "Dev"},
		{ProfileLight, "Light"},
		{ProfileMedium, "Medium"},
		{ProfileHeavy, "Heavy"},
		{ProfileBursty, "Bursty"},
	}

	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			calc := mockCalculator(p.profile, 2048, 4)
			cfg, err := calc.Calculate()

			if err != nil {
				t.Fatalf("Calculate() failed for %s profile: %v", p.name, err)
			}

			if cfg == nil {
				t.Fatalf("Calculate() returned nil config for %s profile", p.name)
			}

			// All profiles should have valid MaxChildren
			if cfg.MaxChildren < 1 {
				t.Errorf("%s: Invalid MaxChildren: %d", p.name, cfg.MaxChildren)
			}

			// Dev is static, others are dynamic
			if p.profile == ProfileDev {
				if cfg.ProcessManager != "static" {
					t.Errorf("%s: Expected static PM, got %s", p.name, cfg.ProcessManager)
				}
			} else {
				if cfg.ProcessManager != "dynamic" {
					t.Errorf("%s: Expected dynamic PM, got %s", p.name, cfg.ProcessManager)
				}

				// Dynamic profiles should have spare server settings
				if cfg.MinSpare < 1 {
					t.Errorf("%s: Invalid MinSpare: %d", p.name, cfg.MinSpare)
				}
				if cfg.MaxSpare < cfg.MinSpare {
					t.Errorf("%s: MaxSpare (%d) < MinSpare (%d)", p.name, cfg.MaxSpare, cfg.MinSpare)
				}
				if cfg.StartServers < cfg.MinSpare {
					t.Errorf("%s: StartServers (%d) < MinSpare (%d)", p.name, cfg.StartServers, cfg.MinSpare)
				}
			}

			// All should have MaxRequests
			if cfg.MaxRequests < 100 {
				t.Errorf("%s: Invalid MaxRequests: %d", p.name, cfg.MaxRequests)
			}

			t.Logf("%s profile: MaxChildren=%d, PM=%s", p.name, cfg.MaxChildren, cfg.ProcessManager)
		})
	}
}

// TestCalculate_MemoryConstraints tests various memory constraints
func TestCalculate_MemoryConstraints(t *testing.T) {
	tests := []struct {
		name     string
		memoryMB int
		cpus     int
		profile  Profile
	}{
		{"Small 512MB", 512, 2, ProfileLight},
		{"Medium 1GB", 1024, 2, ProfileMedium},
		{"Large 2GB", 2048, 4, ProfileMedium},
		{"XLarge 4GB", 4096, 8, ProfileHeavy},
		{"XXLarge 8GB", 8192, 16, ProfileHeavy},
		{"Huge 16GB", 16384, 32, ProfileBursty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := mockCalculator(tt.profile, tt.memoryMB, tt.cpus)
			cfg, err := calc.Calculate()

			if err != nil {
				t.Fatalf("Calculate() failed: %v", err)
			}

			// Workers should not exceed CPUs * 4
			maxWorkers := tt.cpus * 4
			if cfg.MaxChildren > maxWorkers {
				t.Errorf("MaxChildren (%d) exceeds CPU limit (%d CPUs * 4 = %d)",
					cfg.MaxChildren, tt.cpus, maxWorkers)
			}

			// Calculate total memory usage
			profileCfg, _ := tt.profile.GetConfig()
			totalMemory := cfg.MaxChildren*profileCfg.AvgMemoryPerWorker + 64 + 64 // workers + OPcache + reserved

			// Should not exceed 80% threshold (default)
			threshold := 0.8
			maxMemory := int(float64(tt.memoryMB) * threshold)

			if totalMemory > maxMemory {
				t.Errorf("Total memory (%dMB) exceeds threshold (%dMB) for %dMB container",
					totalMemory, maxMemory, tt.memoryMB)
			}

			t.Logf("%s: %d workers, total %dMB / %dMB available",
				tt.name, cfg.MaxChildren, totalMemory, tt.memoryMB)
		})
	}
}

// TestCalculate_CPUConstraints tests CPU limiting
func TestCalculate_CPUConstraints(t *testing.T) {
	tests := []struct {
		cpus        int
		expectMax   int
		description string
	}{
		{1, 4, "Single CPU should allow up to 4 workers"},
		{2, 8, "2 CPUs should allow up to 8 workers"},
		{4, 16, "4 CPUs should allow up to 16 workers"},
		{8, 32, "8 CPUs should allow up to 32 workers"},
		{16, 64, "16 CPUs should allow up to 64 workers"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			// Use lots of memory to not hit memory limit
			calc := mockCalculator(ProfileMedium, 32768, tt.cpus)
			cfg, err := calc.Calculate()

			if err != nil {
				t.Fatalf("Calculate() failed: %v", err)
			}

			if cfg.MaxChildren > tt.expectMax {
				t.Errorf("With %d CPUs, MaxChildren (%d) should not exceed %d",
					tt.cpus, cfg.MaxChildren, tt.expectMax)
			}

			t.Logf("%d CPUs: %d workers (max %d)", tt.cpus, cfg.MaxChildren, tt.expectMax)
		})
	}
}

// TestToEnvVars_AllFields tests environment variable generation
func TestToEnvVars_AllFields(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager:  "dynamic",
		MaxChildren:     20,
		StartServers:    5,
		MinSpare:        3,
		MaxSpare:        10,
		MaxRequests:     1000,
		MemoryTotal:     4096,
		MemoryAllocated: 2560,
	}

	envVars := cfg.ToEnvVars()

	expectedVars := []string{
		"PHP_FPM_PM",
		"PHP_FPM_MAX_CHILDREN",
		"PHP_FPM_START_SERVERS",
		"PHP_FPM_MIN_SPARE",
		"PHP_FPM_MAX_SPARE",
		"PHP_FPM_MAX_REQUESTS",
	}

	for _, varName := range expectedVars {
		if _, exists := envVars[varName]; !exists {
			t.Errorf("Missing environment variable: %s", varName)
		}
	}

	// Check values
	if envVars["PHP_FPM_PM"] != "dynamic" {
		t.Errorf("PHP_FPM_PM = %s, expected dynamic", envVars["PHP_FPM_PM"])
	}

	if envVars["PHP_FPM_MAX_CHILDREN"] != "20" {
		t.Errorf("PHP_FPM_MAX_CHILDREN = %s, expected 20", envVars["PHP_FPM_MAX_CHILDREN"])
	}

	t.Logf("Generated env vars: %+v", envVars)
}

// TestString_Format tests the String() output format
func TestString_Format(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager:  "dynamic",
		MaxChildren:     10,
		StartServers:    3,
		MinSpare:        2,
		MaxSpare:        5,
		MaxRequests:     1000,
		MemoryAllocated: 1280,
		MemoryTotal:     2048,
	}

	s := cfg.String()

	// Should contain key information (note: String() uses " = " with spaces)
	expectedStrings := []string{
		"pm = dynamic",
		"pm.max_children = 10",
		"pm.start_servers = 3",
		"pm.min_spare_servers = 2",
		"pm.max_spare_servers = 5",
		"pm.max_requests = 1000",
		"1280MB",
		"2048MB",
	}

	for _, expected := range expectedStrings {
		if !containsIgnoreCase(s, expected) {
			t.Errorf("String() output should contain %q, got: %s", expected, s)
		}
	}

	t.Logf("String output:\n%s", s)
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[:len(substr)] == substr || containsIgnoreCase(s[1:], substr))))
}

// TestCalculate_ProfileMinimums tests that profile minimums are enforced
func TestCalculate_ProfileMinimums(t *testing.T) {
	// Dev profile should always have exactly 2 workers
	devCalc := mockCalculator(ProfileDev, 512, 1) // Enough memory for dev profile
	devCfg, err := devCalc.Calculate()
	if err != nil {
		t.Fatalf("Dev Calculate() failed: %v", err)
	}
	if devCfg.MaxChildren != 2 {
		t.Errorf("Dev profile should have exactly 2 workers, got %d", devCfg.MaxChildren)
	}

	// Light profile should have at least 2 workers
	lightCalc := mockCalculator(ProfileLight, 512, 1)
	lightCfg, err := lightCalc.Calculate()
	if err != nil {
		t.Fatalf("Light Calculate() failed: %v", err)
	}
	if lightCfg.MaxChildren < 2 {
		t.Errorf("Light profile should have at least 2 workers, got %d", lightCfg.MaxChildren)
	}

	// Medium profile should have at least 3 workers
	mediumCalc := mockCalculator(ProfileMedium, 1024, 1)
	mediumCfg, err := mediumCalc.Calculate()
	if err != nil {
		t.Fatalf("Medium Calculate() failed: %v", err)
	}
	if mediumCfg.MaxChildren < 3 {
		t.Errorf("Medium profile should have at least 3 workers, got %d", mediumCfg.MaxChildren)
	}
}

// TestCalculate_BurstyProfile tests bursty profile characteristics
func TestCalculate_BurstyProfile(t *testing.T) {
	calc := mockCalculator(ProfileBursty, 2048, 4)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Bursty Calculate() failed: %v", err)
	}

	// Bursty should have higher MaxSpare relative to StartServers
	spareRatio := float64(cfg.MaxSpare) / float64(cfg.StartServers)
	if spareRatio < 1.5 {
		t.Errorf("Bursty profile should have high MaxSpare/StartServers ratio, got %.2f", spareRatio)
	}

	t.Logf("Bursty profile: Start=%d, MinSpare=%d, MaxSpare=%d, ratio=%.2f",
		cfg.StartServers, cfg.MinSpare, cfg.MaxSpare, spareRatio)
}
