package autotune

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// mockResources creates a mock ContainerResources for testing
func mockResources(memoryMB, cpus int) *ContainerResources {
	return &ContainerResources{
		MemoryLimitBytes: int64(memoryMB) * 1024 * 1024,
		MemoryLimitMB:    memoryMB,
		CPULimit:         cpus,
		IsContainerized:  true,
		CgroupVersion:    2,
	}
}

// mockCalculator creates a calculator with mocked resources
func mockCalculator(profile Profile, memoryMB, cpus int) *Calculator {
	profileConfig, _ := profile.GetConfig()
	return &Calculator{
		resources:       mockResources(memoryMB, cpus),
		profile:         profileConfig,
		memoryThreshold: 0, // Use profile default
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestCalculator_DevProfile(t *testing.T) {
	calc := mockCalculator(ProfileDev, 512, 2)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Dev profile should use exactly 2 workers (static)
	if cfg.MaxChildren != 2 {
		t.Errorf("Expected 2 workers for dev profile, got %d", cfg.MaxChildren)
	}

	if cfg.ProcessManager != "static" {
		t.Errorf("Expected static PM for dev profile, got %s", cfg.ProcessManager)
	}

	// Memory check: 2 workers * 48MB = 96MB + 64MB OPcache + 64MB reserved = 224MB total
	totalExpected := 2*48 + 64 + 64 // workers + OPcache + reserved
	totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
	if totalUsed > 512 {
		t.Errorf("Memory usage %dMB exceeds available 512MB", totalUsed)
	}

	if totalUsed > totalExpected*2 { // Allow some buffer
		t.Errorf("Memory usage too high: workers=%dMB, opcache=%dMB, reserved=%dMB, total=%dMB",
			cfg.MemoryAllocated, cfg.MemoryOPcache, cfg.MemoryReserved, totalUsed)
	}
}

func TestCalculator_MediumProfile(t *testing.T) {
	// Medium profile: 2GB RAM, 4 CPUs
	calc := mockCalculator(ProfileMedium, 2048, 4)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Available memory: 2048 * 0.75 = 1536MB
	// Reserved: 192MB (system) + 128MB (OPcache) = 320MB
	// Worker memory: 1536 - 320 = 1216MB
	// Workers: 1216 / 42MB = 28.9 → 28 workers
	// CPU limit: 4 CPUs * 4 = 16 max workers (CPU LIMITING!)
	expectedWorkers := 16
	if cfg.MaxChildren != expectedWorkers {
		t.Errorf("Expected %d workers, got %d", expectedWorkers, cfg.MaxChildren)
	}

	if cfg.ProcessManager != "dynamic" {
		t.Errorf("Expected dynamic PM for medium profile, got %s", cfg.ProcessManager)
	}

	// Validate dynamic PM relationships
	if cfg.MinSpare > cfg.MaxSpare {
		t.Errorf("min_spare (%d) > max_spare (%d)", cfg.MinSpare, cfg.MaxSpare)
	}

	if cfg.StartServers < cfg.MinSpare || cfg.StartServers > cfg.MaxSpare {
		t.Errorf("start_servers (%d) not between min_spare (%d) and max_spare (%d)",
			cfg.StartServers, cfg.MinSpare, cfg.MaxSpare)
	}

	// Total memory check (including OPcache)
	totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
	if totalUsed > cfg.MemoryTotal {
		t.Errorf("Total memory usage %dMB exceeds available %dMB", totalUsed, cfg.MemoryTotal)
	}
}

func TestCalculator_HeavyProfile(t *testing.T) {
	// Heavy profile: 8GB RAM, 8 CPUs
	calc := mockCalculator(ProfileHeavy, 8192, 8)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Available memory: 8192 * 0.8 = 6553MB
	// Reserved: 384MB (system) + 256MB (OPcache) = 640MB
	// Worker memory: 6553 - 640 = 5913MB
	// Workers: 5913 / 128MB = 46.1 → 46 workers
	// CPU limit: 8 CPUs * 4 = 32 max workers (limiting!)
	expectedWorkers := 32
	if cfg.MaxChildren != expectedWorkers {
		t.Errorf("Expected %d workers, got %d", expectedWorkers, cfg.MaxChildren)
	}

	// Minimum check from profile
	if cfg.MaxChildren < 8 {
		t.Errorf("Heavy profile should have at least 8 workers, got %d", cfg.MaxChildren)
	}

	// Memory safety (including OPcache)
	totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
	if totalUsed > cfg.MemoryTotal {
		t.Errorf("Memory overprovisioned: %dMB used, %dMB available", totalUsed, cfg.MemoryTotal)
	}
}

func TestCalculator_CPULimit(t *testing.T) {
	// Test CPU limiting: 4GB RAM but only 1 CPU
	calc := mockCalculator(ProfileMedium, 4096, 1)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// CPU limit: 1 CPU * 4 = 4 workers max
	// Memory would allow: (4096*0.75 - 320) / 80 = 34 workers (medium profile)
	// Should be limited to 4 by CPU
	if cfg.MaxChildren > 4 {
		t.Errorf("Expected CPU limit to cap workers at 4, got %d", cfg.MaxChildren)
	}

	if len(cfg.Warnings) == 0 {
		t.Error("Expected warning about CPU limiting, got none")
	}
}

func TestCalculator_InsufficientMemory(t *testing.T) {
	// 64MB is far too small for any profile. The default (non-strict) calculator
	// must not kill PID 1: it clamps to one worker and warns loudly, and the
	// runtime autotuner refines it from there. Strict mode is where a misfit is a
	// hard error (see TestCalculator_StrictModeRefusesAMisfit).
	calc := mockCalculator(ProfileMedium, 64, 2)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("non-strict Calculate() should clamp and boot, not error: %v", err)
	}
	if cfg.MaxChildren < 1 {
		t.Errorf("expected at least 1 worker after clamping, got %d", cfg.MaxChildren)
	}
	if len(cfg.Warnings) == 0 {
		t.Error("expected a loud warning that the container is too small for the profile")
	}
}

func TestCalculator_MinimalMemory(t *testing.T) {
	// Test with 768MB - minimal for light profile
	// Available: 768 * 0.7 = 537MB
	// Reserved: 128MB (system) + 96MB (OPcache) = 224MB
	// Worker memory: 537 - 224 = 313MB
	// Workers: 313 / 64MB = 4.8 → 4 workers
	calc := mockCalculator(ProfileLight, 768, 1)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed with minimal memory: %v", err)
	}

	// Should get at least minimum workers
	if cfg.MaxChildren < 2 {
		t.Errorf("Expected at least 2 workers for light profile, got %d", cfg.MaxChildren)
	}

	// Must not exceed available memory (including OPcache)
	totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
	if totalUsed > cfg.MemoryTotal {
		t.Errorf("Memory overprovisioned: %dMB used, %dMB available", totalUsed, cfg.MemoryTotal)
	}
}

func TestCalculator_BurstyProfile(t *testing.T) {
	// Use more memory to get more workers and show difference
	calc := mockCalculator(ProfileBursty, 4096, 4)
	cfg, err := calc.Calculate()

	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Bursty should have higher spare ratios
	spareRange := cfg.MaxSpare - cfg.MinSpare
	if spareRange < 2 {
		t.Errorf("Expected significant spare range for bursty profile, got min=%d max=%d",
			cfg.MinSpare, cfg.MaxSpare)
	}

	// Bursty should start with at least 50% of max workers
	expectedMinStart := cfg.MaxChildren / 2
	if cfg.StartServers < expectedMinStart {
		t.Errorf("Expected start_servers >= %d (50%% of max_children) for bursty profile, got %d",
			expectedMinStart, cfg.StartServers)
	}
}

func TestPHPFPMConfig_ToEnvVars(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager: "dynamic",
		MaxChildren:    10,
		StartServers:   3,
		MinSpare:       2,
		MaxSpare:       5,
		MaxRequests:    1000,
	}

	env := cfg.ToEnvVars()

	tests := []struct {
		key      string
		expected string
	}{
		{"PHP_FPM_PM", "dynamic"},
		{"PHP_FPM_MAX_CHILDREN", "10"},
		{"PHP_FPM_START_SERVERS", "3"},
		{"PHP_FPM_MIN_SPARE", "2"},
		{"PHP_FPM_MAX_SPARE", "5"},
		{"PHP_FPM_MAX_REQUESTS", "1000"},
	}

	for _, tt := range tests {
		if got := env[tt.key]; got != tt.expected {
			t.Errorf("env[%s] = %s, expected %s", tt.key, got, tt.expected)
		}
	}
}

func TestPHPFPMConfig_ToEnvVars_Static(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager: "static",
		MaxChildren:    2,
		MaxRequests:    100,
	}

	env := cfg.ToEnvVars()

	// Static mode should not have spare/start server vars
	if _, exists := env["PHP_FPM_START_SERVERS"]; exists {
		t.Error("Static mode should not have START_SERVERS env var")
	}

	if _, exists := env["PHP_FPM_MIN_SPARE"]; exists {
		t.Error("Static mode should not have MIN_SPARE env var")
	}
}

func TestCalculator_NoContainerLimits(t *testing.T) {
	// Mock host resources (not containerized)
	profileConfig, _ := ProfileMedium.GetConfig()
	resources := &ContainerResources{
		MemoryLimitBytes: 8 * 1024 * 1024 * 1024, // 8GB host memory
		MemoryLimitMB:    8 * 1024,
		CPULimit:         4,
		IsContainerized:  false, // ← Not in container!
		CgroupVersion:    0,
	}

	calc := &Calculator{
		resources:       resources,
		profile:         profileConfig,
		memoryThreshold: 0, // Use profile default
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()

	// Should succeed but with warnings
	if err != nil {
		t.Errorf("Should not error on host resources, got: %v", err)
	}

	// Should have warning about using host resources
	if len(cfg.Warnings) == 0 {
		t.Error("Expected warning about host resources, got none")
	}

	foundWarning := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "WITHOUT container limits") {
			foundWarning = true
			t.Logf("Warning found: %s", w)
		}
	}

	if !foundWarning {
		t.Error("Expected warning about container limits, but not found")
	}

	// Should still calculate workers (using host memory)
	if cfg.MaxChildren == 0 {
		t.Error("Should calculate workers even without container limits")
	}

	t.Logf("Auto-tuning on host: %d workers with warnings", cfg.MaxChildren)
}

func TestCalculator_MemoryThresholdOverride(t *testing.T) {
	// Test with conservative threshold (60%)
	profileConfig, _ := ProfileMedium.GetConfig()
	calc := &Calculator{
		resources:       mockResources(2048, 4),
		profile:         profileConfig,
		memoryThreshold: 0.6, // 60% instead of profile default (75%)
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// With 60% threshold: 2048 * 0.6 = 1228MB
	// Reserved: 320MB → Worker pool: 908MB
	// Workers: 908 / 42MB = 21 workers
	// CPU limit: 4 * 4 = 16 workers (LIMITED)
	expectedWorkers := 16

	if cfg.MaxChildren != expectedWorkers {
		t.Errorf("Expected %d workers with 60%% threshold, got %d", expectedWorkers, cfg.MaxChildren)
	}

	t.Logf("Conservative threshold (60%%): %d workers", cfg.MaxChildren)
}

func TestCalculator_MemoryThresholdOversubscription(t *testing.T) {
	// Test with oversubscription (130% - DANGEROUS!)
	profileConfig, _ := ProfileMedium.GetConfig()
	calc := &Calculator{
		resources:       mockResources(2048, 4),
		profile:         profileConfig,
		memoryThreshold: 1.3, // 130% - allows oversubscription!
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// With 130% threshold: 2048 * 1.3 = 2662MB (MORE than container has!)
	// Reserved: 320MB → Worker pool: 2342MB
	// Workers: 2342 / 42MB = 55 workers
	// CPU limit: 4 * 4 = 16 workers (LIMITED)
	expectedWorkers := 16

	if cfg.MaxChildren != expectedWorkers {
		t.Errorf("Expected %d workers with oversubscription, got %d", expectedWorkers, cfg.MaxChildren)
	}

	// Should have warning about oversubscription
	foundWarning := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "OVERSUBSCRIPTION") || strings.Contains(w, ">100%") {
			foundWarning = true
			t.Logf("Oversubscription warning: %s", w)
		}
	}

	if !foundWarning {
		t.Error("Expected warning about oversubscription (>100%), but not found")
	}

	t.Logf("Oversubscription threshold (130%%): %d workers with DANGER warning", cfg.MaxChildren)
}

func TestCalculator_MemoryThresholdLow(t *testing.T) {
	// Test with very low threshold (20% - wasteful)
	profileConfig, _ := ProfileMedium.GetConfig()
	calc := &Calculator{
		resources:       mockResources(2048, 4),
		profile:         profileConfig,
		memoryThreshold: 0.2, // 20% - very conservative
		logger:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	cfg, err := calc.Calculate()
	if err != nil {
		t.Fatalf("Calculate() failed: %v", err)
	}

	// Should have warning about being too conservative
	foundWarning := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "very conservative") {
			foundWarning = true
			t.Logf("Conservative warning: %s", w)
		}
	}

	if !foundWarning {
		t.Error("Expected warning about very conservative threshold (<30%), but not found")
	}

	t.Logf("Low threshold (20%%): %d workers with conservation warning", cfg.MaxChildren)
}

func TestCalculator_SafetyValidations(t *testing.T) {
	tests := []struct {
		name      string
		profile   Profile
		memoryMB  int
		cpus      int
		wantError bool
	}{
		{"Sufficient resources", ProfileMedium, 2048, 4, false},
		{"Minimal resources", ProfileLight, 768, 1, false}, // 768*0.7=537, 537-224=313, 313/64=4 workers
		{"Dev tiny", ProfileDev, 384, 1, false},            // 384*0.5=192, 192-128=64, 64/48=1 worker (min 2 enforced)
		// Containers too small for the profile are covered by the clamp/strict tests
		// (calculator_clamp_test.go): non-strict clamps and boots rather than erroring,
		// and the pathological over-commit case intentionally exceeds the limit, so it
		// does not belong in this no-overprovisioning invariant table.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := mockCalculator(tt.profile, tt.memoryMB, tt.cpus)
			cfg, err := calc.Calculate()

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error for %s, got none", tt.name)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Validate no memory overprovisioning (including OPcache)
			totalUsed := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
			if totalUsed > cfg.MemoryTotal {
				t.Errorf("SAFETY VIOLATION: Memory overprovisioned: %dMB used, %dMB available",
					totalUsed, cfg.MemoryTotal)
			}

			// Validate PM relationships
			if cfg.ProcessManager == "dynamic" {
				if cfg.MinSpare > cfg.MaxChildren {
					t.Errorf("SAFETY VIOLATION: min_spare > max_children")
				}
				if cfg.MaxSpare > cfg.MaxChildren {
					t.Errorf("SAFETY VIOLATION: max_spare > max_children")
				}
				if cfg.MinSpare > cfg.MaxSpare {
					t.Errorf("SAFETY VIOLATION: min_spare > max_spare")
				}
			}
		})
	}
}

// Tests for Profile.String()
func TestProfile_String(t *testing.T) {
	tests := []struct {
		profile  Profile
		expected string
	}{
		{ProfileDev, "dev"},
		{ProfileLight, "light"},
		{ProfileMedium, "medium"},
		{ProfileHeavy, "heavy"},
		{ProfileBursty, "bursty"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.profile.String()
			if got != tt.expected {
				t.Errorf("Profile.String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// Tests for PHPFPMConfig.String()
func TestPHPFPMConfig_String_Dynamic(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager:  "dynamic",
		MaxChildren:     10,
		StartServers:    3,
		MinSpare:        2,
		MaxSpare:        5,
		MaxRequests:     1000,
		Profile:         ProfileMedium,
		MemoryAllocated: 420,
		MemoryOPcache:   128,
		MemoryReserved:  192,
		MemoryTotal:     1024,
		CPUs:            4,
	}

	s := cfg.String()

	// Check key elements are present
	if !strings.Contains(s, "medium profile") {
		t.Error("String() should contain profile name")
	}
	if !strings.Contains(s, "pm = dynamic") {
		t.Error("String() should contain process manager type")
	}
	if !strings.Contains(s, "pm.max_children = 10") {
		t.Error("String() should contain max_children")
	}
	if !strings.Contains(s, "pm.start_servers = 3") {
		t.Error("String() should contain start_servers for dynamic PM")
	}
	if !strings.Contains(s, "pm.min_spare_servers = 2") {
		t.Error("String() should contain min_spare_servers for dynamic PM")
	}
	if !strings.Contains(s, "pm.max_spare_servers = 5") {
		t.Error("String() should contain max_spare_servers for dynamic PM")
	}
	if !strings.Contains(s, "pm.max_requests = 1000") {
		t.Error("String() should contain max_requests")
	}
	if !strings.Contains(s, "Memory Breakdown") {
		t.Error("String() should contain memory breakdown section")
	}
	if !strings.Contains(s, "Total Container Memory: 1024MB") {
		t.Error("String() should contain total memory")
	}
	if !strings.Contains(s, "CPUs: 4") {
		t.Error("String() should contain CPU count")
	}
}

func TestPHPFPMConfig_String_Static(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager:  "static",
		MaxChildren:     2,
		MaxRequests:     100,
		Profile:         ProfileDev,
		MemoryAllocated: 96,
		MemoryOPcache:   64,
		MemoryReserved:  64,
		MemoryTotal:     512,
		CPUs:            2,
	}

	s := cfg.String()

	// Check static PM does NOT include spare server settings
	if strings.Contains(s, "pm.start_servers") {
		t.Error("Static PM should not have start_servers in output")
	}
	if strings.Contains(s, "pm.min_spare_servers") {
		t.Error("Static PM should not have min_spare_servers in output")
	}
	if strings.Contains(s, "pm.max_spare_servers") {
		t.Error("Static PM should not have max_spare_servers in output")
	}

	// But should have the basic settings
	if !strings.Contains(s, "pm = static") {
		t.Error("String() should contain pm = static")
	}
	if !strings.Contains(s, "pm.max_children = 2") {
		t.Error("String() should contain max_children")
	}
}

func TestPHPFPMConfig_String_WithWarnings(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager:  "dynamic",
		MaxChildren:     8,
		StartServers:    2,
		MinSpare:        1,
		MaxSpare:        4,
		MaxRequests:     500,
		Profile:         ProfileMedium,
		MemoryAllocated: 256,
		MemoryOPcache:   128,
		MemoryReserved:  192,
		MemoryTotal:     2048,
		CPUs:            2,
		Warnings: []string{
			"CPU limiting: workers limited to 8 by CPU cores",
			"Container limits not detected",
		},
	}

	s := cfg.String()

	if !strings.Contains(s, "Warnings:") {
		t.Error("String() should contain Warnings section when warnings exist")
	}
	if !strings.Contains(s, "CPU limiting") {
		t.Error("String() should include warning text")
	}
	if !strings.Contains(s, "Container limits not detected") {
		t.Error("String() should include all warnings")
	}
}

func TestPHPFPMConfig_String_NoWarnings(t *testing.T) {
	cfg := &PHPFPMConfig{
		ProcessManager:  "dynamic",
		MaxChildren:     10,
		StartServers:    3,
		MinSpare:        2,
		MaxSpare:        5,
		MaxRequests:     1000,
		Profile:         ProfileMedium,
		MemoryAllocated: 420,
		MemoryOPcache:   128,
		MemoryReserved:  192,
		MemoryTotal:     1024,
		CPUs:            4,
		Warnings:        []string{}, // Empty warnings
	}

	s := cfg.String()

	// Should not have warnings section when no warnings
	if strings.Contains(s, "Warnings:") {
		t.Error("String() should not contain Warnings section when no warnings")
	}
}

// Tests for ContainerResources.String()
func TestContainerResources_String_Containerized(t *testing.T) {
	r := &ContainerResources{
		MemoryLimitBytes: 2 * 1024 * 1024 * 1024, // 2GB
		MemoryLimitMB:    2048,
		CPULimit:         4,
		IsContainerized:  true,
		CgroupVersion:    2,
	}

	s := r.String()

	if !strings.Contains(s, "cgroup v2") {
		t.Errorf("String() = %q, expected to contain 'cgroup v2'", s)
	}
	if !strings.Contains(s, "Memory=2048MB") {
		t.Errorf("String() = %q, expected to contain 'Memory=2048MB'", s)
	}
	if !strings.Contains(s, "CPUs=4") {
		t.Errorf("String() = %q, expected to contain 'CPUs=4'", s)
	}
}

func TestContainerResources_String_CgroupV1(t *testing.T) {
	r := &ContainerResources{
		MemoryLimitBytes: 1024 * 1024 * 1024, // 1GB
		MemoryLimitMB:    1024,
		CPULimit:         2,
		IsContainerized:  true,
		CgroupVersion:    1,
	}

	s := r.String()

	if !strings.Contains(s, "cgroup v1") {
		t.Errorf("String() = %q, expected to contain 'cgroup v1'", s)
	}
	if !strings.Contains(s, "Memory=1024MB") {
		t.Errorf("String() = %q, expected to contain 'Memory=1024MB'", s)
	}
}

func TestContainerResources_String_Host(t *testing.T) {
	r := &ContainerResources{
		MemoryLimitBytes: 16 * 1024 * 1024 * 1024, // 16GB
		MemoryLimitMB:    16384,
		CPULimit:         8,
		IsContainerized:  false,
		CgroupVersion:    0,
	}

	s := r.String()

	if !strings.Contains(s, "host") {
		t.Errorf("String() = %q, expected to contain 'host' for non-containerized", s)
	}
	if !strings.Contains(s, "Memory=16384MB") {
		t.Errorf("String() = %q, expected to contain memory amount", s)
	}
	if !strings.Contains(s, "CPUs=8") {
		t.Errorf("String() = %q, expected to contain CPU count", s)
	}
}

func TestContainerResources_String_UnlimitedMemory(t *testing.T) {
	r := &ContainerResources{
		MemoryLimitBytes: 0, // Unlimited
		MemoryLimitMB:    0,
		CPULimit:         4,
		IsContainerized:  false,
		CgroupVersion:    0,
	}

	s := r.String()

	if !strings.Contains(s, "Memory=unlimited") {
		t.Errorf("String() = %q, expected to contain 'Memory=unlimited' for zero memory", s)
	}
}

// Test Profile.Validate()
func TestProfile_Validate(t *testing.T) {
	tests := []struct {
		profile   Profile
		wantError bool
	}{
		{ProfileDev, false},
		{ProfileLight, false},
		{ProfileMedium, false},
		{ProfileHeavy, false},
		{ProfileBursty, false},
		{Profile("invalid"), true},
		{Profile(""), true},
		{Profile("MEDIUM"), true}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			err := tt.profile.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Profile(%q).Validate() error = %v, wantError = %v", tt.profile, err, tt.wantError)
			}
		})
	}
}

// Test Profile.GetConfig()
func TestProfile_GetConfig(t *testing.T) {
	tests := []struct {
		profile   Profile
		wantError bool
	}{
		{ProfileDev, false},
		{ProfileLight, false},
		{ProfileMedium, false},
		{ProfileHeavy, false},
		{ProfileBursty, false},
		{Profile("invalid"), true},
	}

	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			cfg, err := tt.profile.GetConfig()
			if (err != nil) != tt.wantError {
				t.Errorf("Profile(%q).GetConfig() error = %v, wantError = %v", tt.profile, err, tt.wantError)
			}
			if !tt.wantError && cfg.Name == "" {
				t.Error("GetConfig() returned empty config name for valid profile")
			}
		})
	}
}

// Test ProfileConfig values
func TestProfileConfig_Values(t *testing.T) {
	tests := []struct {
		profile           Profile
		expectedMinWorker int
		expectedPMType    string
	}{
		{ProfileDev, 2, "static"},
		{ProfileLight, 2, "dynamic"},
		{ProfileMedium, 4, "dynamic"},
		{ProfileHeavy, 8, "dynamic"},
		{ProfileBursty, 4, "dynamic"},
	}

	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			cfg, err := tt.profile.GetConfig()
			if err != nil {
				t.Fatalf("GetConfig() failed: %v", err)
			}

			if cfg.MinWorkers != tt.expectedMinWorker {
				t.Errorf("MinWorkers = %d, expected %d", cfg.MinWorkers, tt.expectedMinWorker)
			}
			if cfg.ProcessManagerType != tt.expectedPMType {
				t.Errorf("ProcessManagerType = %q, expected %q", cfg.ProcessManagerType, tt.expectedPMType)
			}
		})
	}
}
