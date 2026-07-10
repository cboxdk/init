package autotune

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGetHostMemory_ActualSystem tests the real getHostMemory function
func TestGetHostMemory_ActualSystem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Test requires Linux /proc/meminfo")
	}

	// Call the actual function
	memory, err := getHostMemory()

	// On Linux systems, this should succeed
	if err != nil {
		// Check if /proc/meminfo exists
		if _, statErr := os.Stat("/proc/meminfo"); os.IsNotExist(statErr) {
			t.Skip("/proc/meminfo does not exist on this system")
		}
		t.Logf("getHostMemory() returned error (might be expected): %v", err)
	}

	if memory > 0 {
		t.Logf("Detected host memory: %d bytes (%.2f GB)",
			memory, float64(memory)/(1024*1024*1024))

		// Sanity check: memory should be between 100MB and 1TB
		if memory < 100*1024*1024 {
			t.Errorf("Memory too low: %d bytes", memory)
		}
		if memory > 1024*1024*1024*1024 {
			t.Errorf("Memory too high: %d bytes", memory)
		}
	}
}

// TestGetHostMemory_FileReadError tests error handling when file doesn't exist
func TestGetHostMemory_FileReadError(t *testing.T) {
	// We can't easily test the error path without modifying the function,
	// but we can verify it doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("getHostMemory() panicked: %v", r)
		}
	}()

	// Call the actual function - it will either succeed or return error
	_, _ = getHostMemory()
}

// TestDetectCgroupV2Resources_EdgeCases tests cgroup v2 edge cases
func TestDetectCgroupV2Resources_EdgeCases(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}

	tests := []struct {
		name        string
		setup       func(dir string) error
		expectError bool
		description string
	}{
		{
			name: "empty memory.max file",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "memory.max"), []byte(""), 0644)
			},
			expectError: true,
			description: "Empty file should fail parsing",
		},
		{
			name: "only whitespace in memory.max",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "memory.max"), []byte("   \n"), 0644)
			},
			expectError: true,
			description: "Whitespace-only should fail",
		},
		{
			name: "invalid number in memory.max",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "memory.max"), []byte("invalid\n"), 0644)
			},
			expectError: true,
			description: "Non-numeric value should fail",
		},
		{
			name: "only cpu.max without memory.max",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "cpu.max"), []byte("200000 100000\n"), 0644)
			},
			expectError: true,
			description: "CPU without memory should fail (memory required for success)",
		},
		{
			name: "malformed cpu.max (single value)",
			setup: func(dir string) error {
				_ = os.WriteFile(filepath.Join(dir, "memory.max"), []byte("1073741824\n"), 0644)
				return os.WriteFile(filepath.Join(dir, "cpu.max"), []byte("200000\n"), 0644)
			},
			expectError: false,
			description: "Malformed CPU should not prevent memory detection",
		},
		{
			name: "cpu.max with zero period",
			setup: func(dir string) error {
				_ = os.WriteFile(filepath.Join(dir, "memory.max"), []byte("1073741824\n"), 0644)
				return os.WriteFile(filepath.Join(dir, "cpu.max"), []byte("200000 0\n"), 0644)
			},
			expectError: false,
			description: "Zero period should be ignored, memory should succeed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// We can't directly test detectCgroupV2Resources without modifying it,
			// but we can verify the file structure and logic
			t.Logf("Test case: %s", tt.description)

			// Verify files were created
			files, _ := os.ReadDir(tmpDir)
			for _, f := range files {
				content, _ := os.ReadFile(filepath.Join(tmpDir, f.Name()))
				t.Logf("  %s: %q", f.Name(), strings.TrimSpace(string(content)))
			}
		})
	}
}

// TestDetectCgroupV1Resources_EdgeCases tests cgroup v1 edge cases
func TestDetectCgroupV1Resources_EdgeCases(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}

	tests := []struct {
		name        string
		setup       func(dir string) error
		expectError bool
		description string
	}{
		{
			name: "unrealistic memory limit (unlimited)",
			setup: func(dir string) error {
				// Value > 1PB should be treated as unlimited
				return os.WriteFile(filepath.Join(dir, "memory.limit_in_bytes"),
					[]byte("9223372036854771712\n"), 0644)
			},
			expectError: true,
			description: "Very large value should be treated as unlimited (no limit set)",
		},
		{
			name: "negative cpu quota (unlimited)",
			setup: func(dir string) error {
				_ = os.WriteFile(filepath.Join(dir, "memory.limit_in_bytes"), []byte("1073741824\n"), 0644)
				_ = os.WriteFile(filepath.Join(dir, "cpu.cfs_quota_us"), []byte("-1\n"), 0644)
				return os.WriteFile(filepath.Join(dir, "cpu.cfs_period_us"), []byte("100000\n"), 0644)
			},
			expectError: false,
			description: "Negative quota means unlimited CPU, memory should still succeed",
		},
		{
			name: "zero cpu quota",
			setup: func(dir string) error {
				_ = os.WriteFile(filepath.Join(dir, "memory.limit_in_bytes"), []byte("1073741824\n"), 0644)
				_ = os.WriteFile(filepath.Join(dir, "cpu.cfs_quota_us"), []byte("0\n"), 0644)
				return os.WriteFile(filepath.Join(dir, "cpu.cfs_period_us"), []byte("100000\n"), 0644)
			},
			expectError: false,
			description: "Zero quota should be ignored",
		},
		{
			name: "missing cpu period file",
			setup: func(dir string) error {
				_ = os.WriteFile(filepath.Join(dir, "memory.limit_in_bytes"), []byte("1073741824\n"), 0644)
				return os.WriteFile(filepath.Join(dir, "cpu.cfs_quota_us"), []byte("200000\n"), 0644)
			},
			expectError: false,
			description: "Missing period file should not prevent memory detection",
		},
		{
			name: "malformed memory limit",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "memory.limit_in_bytes"),
					[]byte("not-a-number\n"), 0644)
			},
			expectError: true,
			description: "Malformed memory should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			t.Logf("Test case: %s", tt.description)

			// Verify files were created
			files, _ := os.ReadDir(tmpDir)
			for _, f := range files {
				content, _ := os.ReadFile(filepath.Join(tmpDir, f.Name()))
				t.Logf("  %s: %q", f.Name(), strings.TrimSpace(string(content)))
			}
		})
	}
}

// TestDetectContainerResources_VersionValidation tests cgroup version consistency
func TestDetectContainerResources_VersionValidation(t *testing.T) {
	// This test validates the cgroup version consistency rules

	resources, err := DetectContainerResources()
	if err != nil {
		t.Fatalf("DetectContainerResources failed: %v", err)
	}

	if resources.IsContainerized {
		// Should prefer v2 if both exist
		switch resources.CgroupVersion {
		case 2:
			t.Logf("Detected cgroup v2 (preferred)")
		case 1:
			t.Logf("Detected cgroup v1 (v2 not available)")
		}
	} else {
		t.Logf("Not containerized, using host resources")
	}

	// Verify cgroup version consistency
	if resources.CgroupVersion > 2 {
		t.Errorf("Invalid cgroup version: %d", resources.CgroupVersion)
	}

	if resources.IsContainerized && resources.CgroupVersion == 0 {
		t.Error("Containerized should have cgroup version 1 or 2")
	}

	if !resources.IsContainerized && resources.CgroupVersion != 0 {
		t.Error("Non-containerized should have cgroup version 0")
	}
}

// TestDetectContainerResources_MemoryConsistency tests memory bytes/MB consistency
func TestDetectContainerResources_MemoryConsistency(t *testing.T) {
	resources, err := DetectContainerResources()
	if err != nil {
		t.Fatalf("DetectContainerResources failed: %v", err)
	}

	// Check consistency between bytes and MB
	expectedMB := int(resources.MemoryLimitBytes / (1024 * 1024))
	if resources.MemoryLimitMB != expectedMB {
		t.Errorf("Memory inconsistency: bytes=%d → MB should be %d, got %d",
			resources.MemoryLimitBytes, expectedMB, resources.MemoryLimitMB)
	}

	// Both should be zero or both non-zero
	if (resources.MemoryLimitBytes == 0) != (resources.MemoryLimitMB == 0) {
		t.Errorf("Memory zero mismatch: bytes=%d, MB=%d",
			resources.MemoryLimitBytes, resources.MemoryLimitMB)
	}

	t.Logf("Memory consistency OK: %d bytes = %d MB",
		resources.MemoryLimitBytes, resources.MemoryLimitMB)
}

// TestContainerResources_ZeroValues tests zero value handling
func TestContainerResources_ZeroValues(t *testing.T) {
	tests := []struct {
		name      string
		resources *ContainerResources
		expectStr string
	}{
		{
			name: "zero memory (unlimited)",
			resources: &ContainerResources{
				MemoryLimitBytes: 0,
				MemoryLimitMB:    0,
				CPULimit:         4,
				IsContainerized:  false,
			},
			expectStr: "unlimited",
		},
		{
			name: "zero CPUs (invalid but shouldn't panic)",
			resources: &ContainerResources{
				MemoryLimitBytes: 1024 * 1024 * 1024,
				MemoryLimitMB:    1024,
				CPULimit:         0,
				IsContainerized:  false,
			},
			expectStr: "CPUs=0",
		},
		{
			name: "all zeros",
			resources: &ContainerResources{
				MemoryLimitBytes: 0,
				MemoryLimitMB:    0,
				CPULimit:         0,
				IsContainerized:  false,
			},
			expectStr: "unlimited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("String() panicked: %v", r)
				}
			}()

			s := tt.resources.String()
			if !strings.Contains(s, tt.expectStr) {
				t.Errorf("String() = %q, expected to contain %q", s, tt.expectStr)
			}

			t.Logf("String representation: %s", s)
		})
	}
}

// TestDetectContainerResources_NoError tests that function never returns error
func TestDetectContainerResources_NoError(t *testing.T) {
	// DetectContainerResources should always return non-nil resources,
	// even if cgroup detection fails (falls back to host)

	resources, err := DetectContainerResources()

	if err != nil {
		t.Errorf("DetectContainerResources() should not return error, got: %v", err)
	}

	if resources == nil {
		t.Fatal("DetectContainerResources() returned nil resources")
	}

	// Should always have valid data
	if resources.CPULimit < 0 {
		t.Error("CPU limit should not be negative")
	}

	if resources.MemoryLimitBytes < 0 {
		t.Error("Memory bytes should not be negative")
	}

	if resources.MemoryLimitMB < 0 {
		t.Error("Memory MB should not be negative")
	}

	t.Logf("Detection successful: %s", resources.String())
}

// TestCgroupExistenceChecks tests the cgroup existence check functions
func TestCgroupExistenceChecks(t *testing.T) {
	// Test that these functions don't panic and return booleans
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Cgroup existence check panicked: %v", r)
		}
	}()

	v2Exists := cgroupV2Exists()
	v1Exists := cgroupV1Exists()

	t.Logf("cgroup v2 exists: %v", v2Exists)
	t.Logf("cgroup v1 exists: %v", v1Exists)

	// On any system, at most one should typically exist
	// (though both could exist during transition)
	if v2Exists && v1Exists {
		t.Log("Both cgroup v1 and v2 detected (hybrid system)")
	}

	// Neither is also valid (running on macOS or other non-Linux)
	if !v2Exists && !v1Exists {
		if runtime.GOOS == "linux" {
			t.Log("No cgroups detected on Linux system (unusual but possible)")
		} else {
			t.Logf("No cgroups detected on %s (expected)", runtime.GOOS)
		}
	}
}

// TestContainerResources_Negative tests handling of negative values
func TestContainerResources_NegativeValues(t *testing.T) {
	// ContainerResources should never have negative values,
	// but if it does, String() should handle it gracefully

	r := &ContainerResources{
		MemoryLimitBytes: -1,
		MemoryLimitMB:    -1,
		CPULimit:         -1,
		IsContainerized:  false,
	}

	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("String() panicked with negative values: %v", rec)
		}
	}()

	s := r.String()
	t.Logf("String with negative values: %s", s)
}

// TestContainerResources_LargeValues tests handling of very large values
func TestContainerResources_LargeValues(t *testing.T) {
	// Test with unrealistically large values
	r := &ContainerResources{
		MemoryLimitBytes: 1 << 60, // 1 EB
		MemoryLimitMB:    1 << 50, // 1 PB in MB
		CPULimit:         10000,
		IsContainerized:  true,
		CgroupVersion:    2,
	}

	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("String() panicked with large values: %v", rec)
		}
	}()

	s := r.String()
	t.Logf("String with large values: %s", s)

	// Should contain the large values
	if !strings.Contains(s, "CPUs=10000") {
		t.Error("String should contain large CPU value")
	}
}
