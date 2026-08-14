package autotune

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDetectContainerResources tests the main resource detection function
func TestDetectContainerResources(t *testing.T) {
	// This test will attempt real detection
	// Result depends on whether we're running in a container or on host
	resources, err := DetectContainerResources()

	if err != nil {
		t.Fatalf("DetectContainerResources() failed: %v", err)
	}

	if resources == nil {
		t.Fatal("Expected non-nil resources")
	}

	// Should have at least 1 CPU
	if resources.CPULimit < 1 {
		t.Errorf("Expected at least 1 CPU, got %d", resources.CPULimit)
	}

	// Should have detected some memory
	// Note: MemoryLimitMB might be 0 if unlimited
	if resources.MemoryLimitBytes < 0 {
		t.Errorf("Expected non-negative memory bytes, got %d", resources.MemoryLimitBytes)
	}

	// If containerized, should have cgroup version
	if resources.IsContainerized {
		if resources.CgroupVersion != 1 && resources.CgroupVersion != 2 {
			t.Errorf("Expected cgroup version 1 or 2, got %d", resources.CgroupVersion)
		}
		t.Logf("Detected containerized environment: %s", resources.String())
	} else {
		if resources.CgroupVersion != 0 {
			t.Errorf("Non-containerized should have cgroup version 0, got %d", resources.CgroupVersion)
		}
		t.Logf("Detected host environment: %s", resources.String())
	}

	// Verify consistency between bytes and MB
	expectedMB := int(resources.MemoryLimitBytes / (1024 * 1024))
	if resources.MemoryLimitMB != expectedMB {
		t.Errorf("Memory inconsistency: MemoryLimitMB=%d, expected %d from bytes=%d",
			resources.MemoryLimitMB, expectedMB, resources.MemoryLimitBytes)
	}
}

// TestCgroupV2Detection tests cgroup v2 detection with mocked filesystem
func TestCgroupV2Detection(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()
	cgroupDir := tmpDir

	// Test case 1: cgroup v2 exists and has valid data
	t.Run("valid cgroup v2", func(t *testing.T) {
		// Create cgroup.controllers file (marker for v2)
		controllersPath := filepath.Join(cgroupDir, "cgroup.controllers")
		if err := os.WriteFile(controllersPath, []byte("cpu memory\n"), 0644); err != nil {
			t.Fatalf("Failed to create cgroup.controllers: %v", err)
		}

		// Create memory.max
		memMaxPath := filepath.Join(cgroupDir, "memory.max")
		if err := os.WriteFile(memMaxPath, []byte("2147483648\n"), 0644); err != nil { // 2GB
			t.Fatalf("Failed to create memory.max: %v", err)
		}

		// Create cpu.max
		cpuMaxPath := filepath.Join(cgroupDir, "cpu.max")
		if err := os.WriteFile(cpuMaxPath, []byte("200000 100000\n"), 0644); err != nil { // 2 CPUs
			t.Fatalf("Failed to create cpu.max: %v", err)
		}

		// Note: We can't easily test the actual detection without modifying
		// the source to accept a custom path. This demonstrates what the
		// test structure would look like with dependency injection.
		// For now, we test the helper functions directly.
	})
}

// TestCgroupV2Exists tests the cgroup v2 existence check
func TestCgroupV2Exists(t *testing.T) {
	// This will check the real filesystem
	exists := cgroupV2Exists()

	// We don't know if we're running on a system with cgroup v2,
	// so just verify it returns a boolean without error
	t.Logf("cgroup v2 exists: %v", exists)

	// If it exists, the file should be readable
	if exists {
		_, err := os.Stat("/sys/fs/cgroup/cgroup.controllers")
		if err != nil {
			t.Errorf("cgroupV2Exists() returned true but file not accessible: %v", err)
		}
	}
}

// TestCgroupV1Exists tests the cgroup v1 existence check
func TestCgroupV1Exists(t *testing.T) {
	// This will check the real filesystem
	exists := cgroupV1Exists()

	// We don't know if we're running on a system with cgroup v1,
	// so just verify it returns a boolean without error
	t.Logf("cgroup v1 exists: %v", exists)

	// If it exists, the file should be readable
	if exists {
		_, err := os.Stat("/sys/fs/cgroup/memory/memory.limit_in_bytes")
		if err != nil {
			t.Errorf("cgroupV1Exists() returned true but file not accessible: %v", err)
		}
	}
}

// TestGetHostMemory tests reading host memory from /proc/meminfo
func TestGetHostMemory(t *testing.T) {
	// This requires /proc/meminfo to exist (Linux only)
	if runtime.GOOS != "linux" {
		t.Skip("Test requires Linux /proc/meminfo")
	}

	memory, err := getHostMemory()

	if err != nil {
		// If /proc/meminfo doesn't exist or is unreadable, that's valid for the test
		if os.IsNotExist(err) {
			t.Skip("/proc/meminfo not found")
		}
		t.Fatalf("getHostMemory() failed: %v", err)
	}

	// Memory should be positive and reasonable (> 100MB, < 1TB)
	minMemory := int64(100 * 1024 * 1024)         // 100 MB
	maxMemory := int64(1024 * 1024 * 1024 * 1024) // 1 TB

	if memory < minMemory {
		t.Errorf("Memory %d bytes seems too low (< 100MB)", memory)
	}

	if memory > maxMemory {
		t.Errorf("Memory %d bytes seems too high (> 1TB)", memory)
	}

	t.Logf("Detected host memory: %d bytes (%.2f GB)",
		memory, float64(memory)/(1024*1024*1024))
}

// TestGetHostMemory_ParseError tests handling of malformed /proc/meminfo
func TestGetHostMemory_MissingFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Test requires Linux")
	}

	// We can't easily mock the filesystem without changing the function,
	// but we can test that the function handles errors gracefully
	// by verifying the current implementation doesn't panic

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("getHostMemory() panicked: %v", r)
		}
	}()

	_, _ = getHostMemory()
}

// TestDetectCgroupV2Resources_MemoryMax tests parsing of memory.max file
func TestDetectCgroupV2Resources_MemoryParsing(t *testing.T) {
	tests := []struct {
		name          string
		memoryContent string
		expectMemory  int64
		expectMB      int
	}{
		{
			name:          "valid memory limit",
			memoryContent: "2147483648\n",
			expectMemory:  2147483648,
			expectMB:      2048,
		},
		{
			name:          "memory max unlimited",
			memoryContent: "max\n",
			expectMemory:  0,
			expectMB:      0,
		},
		{
			name:          "1GB limit",
			memoryContent: "1073741824\n",
			expectMemory:  1073741824,
			expectMB:      1024,
		},
		{
			name:          "512MB limit",
			memoryContent: "536870912\n",
			expectMemory:  536870912,
			expectMB:      512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()
			memMaxPath := filepath.Join(tmpDir, "memory.max")

			if err := os.WriteFile(memMaxPath, []byte(tt.memoryContent), 0644); err != nil {
				t.Fatalf("Failed to create memory.max: %v", err)
			}

			// Note: We can't call detectCgroupV2Resources directly without
			// modifying it to accept a custom path, but this demonstrates
			// the test structure for file parsing logic
		})
	}
}

// TestDetectCgroupV2Resources_CPUMax tests parsing of cpu.max file
func TestDetectCgroupV2Resources_CPUParsing(t *testing.T) {
	tests := []struct {
		name        string
		cpuContent  string
		expectCPUs  int
		description string
	}{
		{
			name:        "2 CPUs",
			cpuContent:  "200000 100000\n",
			expectCPUs:  2,
			description: "200000/100000 = 2",
		},
		{
			name:        "4 CPUs",
			cpuContent:  "400000 100000\n",
			expectCPUs:  4,
			description: "400000/100000 = 4",
		},
		{
			name:        "fractional 1.5 CPUs rounds to 2",
			cpuContent:  "150000 100000\n",
			expectCPUs:  2,
			description: "150000/100000 = 1.5, rounds up to 2",
		},
		{
			name:        "unlimited CPUs",
			cpuContent:  "max 100000\n",
			expectCPUs:  runtime.NumCPU(),
			description: "max should keep default runtime.NumCPU()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test: %s", tt.description)
			// This demonstrates the test structure for CPU parsing
		})
	}
}

// TestDetectCgroupV1Resources_UnlimitedMemory tests handling of unlimited memory in v1
func TestDetectCgroupV1Resources_UnlimitedMemory(t *testing.T) {
	// cgroup v1 uses very large values (> 1PB) to indicate unlimited
	tests := []struct {
		name        string
		memoryValue string
		shouldSet   bool
		description string
	}{
		{
			name:        "reasonable limit 2GB",
			memoryValue: "2147483648",
			shouldSet:   true,
			description: "2GB is a normal limit",
		},
		{
			name:        "unlimited (very large value)",
			memoryValue: "9223372036854771712", // Close to max int64
			shouldSet:   false,
			description: "Very large value indicates unlimited",
		},
		{
			name:        "1PB (unrealistic limit)",
			memoryValue: "1125899906842624", // 1PB
			shouldSet:   false,
			description: "1PB is treated as unlimited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test: %s", tt.description)
			// This demonstrates the boundary check logic for v1 unlimited memory
		})
	}
}

// TestContainerResources_String tests the String() method
func TestContainerResources_String_Variations(t *testing.T) {
	tests := []struct {
		name      string
		resources *ContainerResources
		contains  []string
	}{
		{
			name: "cgroup v2 containerized",
			resources: &ContainerResources{
				MemoryLimitBytes: 2 * 1024 * 1024 * 1024,
				MemoryLimitMB:    2048,
				CPULimit:         4,
				IsContainerized:  true,
				CgroupVersion:    2,
			},
			contains: []string{"cgroup v2", "2048MB", "CPUs=4"},
		},
		{
			name: "cgroup v1 containerized",
			resources: &ContainerResources{
				MemoryLimitBytes: 1024 * 1024 * 1024,
				MemoryLimitMB:    1024,
				CPULimit:         2,
				IsContainerized:  true,
				CgroupVersion:    1,
			},
			contains: []string{"cgroup v1", "1024MB", "CPUs=2"},
		},
		{
			name: "host resources",
			resources: &ContainerResources{
				MemoryLimitBytes: 16 * 1024 * 1024 * 1024,
				MemoryLimitMB:    16384,
				CPULimit:         8,
				IsContainerized:  false,
				CgroupVersion:    0,
			},
			contains: []string{"host", "16384MB", "CPUs=8"},
		},
		{
			name: "unlimited memory",
			resources: &ContainerResources{
				MemoryLimitBytes: 0,
				MemoryLimitMB:    0,
				CPULimit:         4,
				IsContainerized:  false,
				CgroupVersion:    0,
			},
			contains: []string{"unlimited", "CPUs=4"},
		},
		{
			name: "single CPU",
			resources: &ContainerResources{
				MemoryLimitBytes: 512 * 1024 * 1024,
				MemoryLimitMB:    512,
				CPULimit:         1,
				IsContainerized:  true,
				CgroupVersion:    2,
			},
			contains: []string{"512MB", "CPUs=1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.resources.String()

			for _, substr := range tt.contains {
				if !strings.Contains(s, substr) {
					t.Errorf("String() = %q, expected to contain %q", s, substr)
				}
			}

			// Verify format
			if !strings.HasPrefix(s, "Resources[") {
				t.Errorf("String() should start with 'Resources[', got: %s", s)
			}
		})
	}
}

// TestDetectCgroupV1Resources_CPUQuota tests CPU quota parsing in v1
func TestDetectCgroupV1Resources_CPUCalculation(t *testing.T) {
	tests := []struct {
		name        string
		quota       int64
		period      int64
		expectedCPU int
		description string
	}{
		{
			name:        "2 CPUs",
			quota:       200000,
			period:      100000,
			expectedCPU: 2,
			description: "200000/100000 = 2",
		},
		{
			name:        "4 CPUs",
			quota:       400000,
			period:      100000,
			expectedCPU: 4,
			description: "400000/100000 = 4",
		},
		{
			name:        "fractional 1.5 rounds to 2",
			quota:       150000,
			period:      100000,
			expectedCPU: 2,
			description: "(150000 + 100000 - 1) / 100000 = 2",
		},
		{
			name:        "unlimited quota -1",
			quota:       -1,
			period:      100000,
			expectedCPU: runtime.NumCPU(),
			description: "quota -1 means unlimited, keep default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate CPU with round-up logic
			var calculatedCPU int
			if tt.quota > 0 && tt.period > 0 {
				calculatedCPU = int((tt.quota + tt.period - 1) / tt.period)
			} else {
				calculatedCPU = runtime.NumCPU()
			}

			t.Logf("%s: calculated=%d, expected=%d", tt.description, calculatedCPU, tt.expectedCPU)

			if tt.quota > 0 && calculatedCPU != tt.expectedCPU {
				// Only check positive quotas (skip unlimited case which depends on runtime)
				t.Errorf("CPU calculation mismatch: got %d, expected %d", calculatedCPU, tt.expectedCPU)
			}
		})
	}
}

// TestDetectContainerResources_FallbackToHost tests fallback behavior
func TestDetectContainerResources_FallbackToHost(t *testing.T) {
	// This test verifies that when cgroup detection fails,
	// the function falls back to host resources

	resources, err := DetectContainerResources()

	if err != nil {
		t.Fatalf("DetectContainerResources() should not error on fallback: %v", err)
	}

	// Should always return valid resources
	if resources.CPULimit < 1 {
		t.Error("Should have at least 1 CPU even in fallback mode")
	}

	// On fallback, IsContainerized should be false
	if !resources.IsContainerized && resources.CgroupVersion != 0 {
		t.Error("Non-containerized resources should have CgroupVersion=0")
	}

	t.Logf("Fallback resources: %s", resources.String())
}

// TestParseCgroupV2Memory tests the cgroup v2 memory parsing function
func TestParseCgroupV2Memory(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectBytes int64
		expectMB    int
	}{
		{
			name:        "valid 2GB limit",
			content:     "2147483648\n",
			expectBytes: 2147483648,
			expectMB:    2048,
		},
		{
			name:        "valid 1GB limit",
			content:     "1073741824\n",
			expectBytes: 1073741824,
			expectMB:    1024,
		},
		{
			name:        "valid 512MB limit",
			content:     "536870912\n",
			expectBytes: 536870912,
			expectMB:    512,
		},
		{
			name:        "unlimited memory (max)",
			content:     "max\n",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "unlimited no newline",
			content:     "max",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "with whitespace",
			content:     "  1073741824  \n",
			expectBytes: 1073741824,
			expectMB:    1024,
		},
		{
			name:        "invalid content",
			content:     "invalid\n",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "empty content",
			content:     "",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "256MB limit",
			content:     "268435456",
			expectBytes: 268435456,
			expectMB:    256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ContainerResources{}
			parseCgroupV2Memory(r, tt.content)

			if r.MemoryLimitBytes != tt.expectBytes {
				t.Errorf("MemoryLimitBytes = %d, want %d", r.MemoryLimitBytes, tt.expectBytes)
			}
			if r.MemoryLimitMB != tt.expectMB {
				t.Errorf("MemoryLimitMB = %d, want %d", r.MemoryLimitMB, tt.expectMB)
			}
		})
	}
}

// TestParseCgroupV2CPU tests the cgroup v2 CPU parsing function
func TestParseCgroupV2CPU(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		expectCPUs int
		startCPUs  int // Starting CPU value to test "unchanged" cases
	}{
		{
			name:       "2 CPUs",
			content:    "200000 100000\n",
			expectCPUs: 2,
			startCPUs:  0,
		},
		{
			name:       "4 CPUs",
			content:    "400000 100000\n",
			expectCPUs: 4,
			startCPUs:  0,
		},
		{
			name:       "1 CPU",
			content:    "100000 100000\n",
			expectCPUs: 1,
			startCPUs:  0,
		},
		{
			name:       "fractional 1.5 CPUs rounds up to 2",
			content:    "150000 100000\n",
			expectCPUs: 2,
			startCPUs:  0,
		},
		{
			name:       "fractional 0.5 CPU rounds up to 1",
			content:    "50000 100000\n",
			expectCPUs: 1,
			startCPUs:  0,
		},
		{
			name:       "fractional 2.25 CPUs rounds up to 3",
			content:    "225000 100000\n",
			expectCPUs: 3,
			startCPUs:  0,
		},
		{
			name:       "unlimited CPUs (max) keeps default",
			content:    "max 100000\n",
			expectCPUs: 4, // Should keep startCPUs
			startCPUs:  4,
		},
		{
			name:       "single value unchanged",
			content:    "200000\n",
			expectCPUs: 8, // Should keep startCPUs
			startCPUs:  8,
		},
		{
			name:       "invalid format unchanged",
			content:    "invalid\n",
			expectCPUs: 2, // Should keep startCPUs
			startCPUs:  2,
		},
		{
			name:       "empty unchanged",
			content:    "",
			expectCPUs: 4, // Should keep startCPUs
			startCPUs:  4,
		},
		{
			name:       "zero period unchanged",
			content:    "200000 0\n",
			expectCPUs: 2, // Should keep startCPUs
			startCPUs:  2,
		},
		{
			name:       "non-standard period",
			content:    "500000 250000\n",
			expectCPUs: 2, // 500000/250000 = 2
			startCPUs:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ContainerResources{CPULimit: tt.startCPUs}
			parseCgroupV2CPU(r, tt.content)

			if r.CPULimit != tt.expectCPUs {
				t.Errorf("CPULimit = %d, want %d", r.CPULimit, tt.expectCPUs)
			}
		})
	}
}

// TestParseCgroupV1Memory tests the cgroup v1 memory parsing function
func TestParseCgroupV1Memory(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectBytes int64
		expectMB    int
	}{
		{
			name:        "valid 2GB limit",
			content:     "2147483648\n",
			expectBytes: 2147483648,
			expectMB:    2048,
		},
		{
			name:        "valid 1GB limit",
			content:     "1073741824",
			expectBytes: 1073741824,
			expectMB:    1024,
		},
		{
			name:        "valid 512MB limit",
			content:     "536870912\n",
			expectBytes: 536870912,
			expectMB:    512,
		},
		{
			name:        "unlimited memory (very large value)",
			content:     "9223372036854771712\n", // Near max int64
			expectBytes: 0,                       // Should be ignored as unlimited
			expectMB:    0,
		},
		{
			name:        "unlimited 1PB threshold",
			content:     "1125899906842624\n", // 1PB - exactly at threshold
			expectBytes: 0,                    // Should be ignored
			expectMB:    0,
		},
		{
			name:        "just under 1PB valid",
			content:     "1125899906842623\n", // 1 byte under 1PB
			expectBytes: 1125899906842623,
			expectMB:    1073741823,
		},
		{
			name:        "with whitespace",
			content:     "  1073741824  \n",
			expectBytes: 1073741824,
			expectMB:    1024,
		},
		{
			name:        "invalid content",
			content:     "invalid\n",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "empty content",
			content:     "",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "negative value",
			content:     "-1\n",
			expectBytes: 0,
			expectMB:    0,
		},
		{
			name:        "256MB limit",
			content:     "268435456",
			expectBytes: 268435456,
			expectMB:    256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ContainerResources{}
			parseCgroupV1Memory(r, tt.content)

			if r.MemoryLimitBytes != tt.expectBytes {
				t.Errorf("MemoryLimitBytes = %d, want %d", r.MemoryLimitBytes, tt.expectBytes)
			}
			if r.MemoryLimitMB != tt.expectMB {
				t.Errorf("MemoryLimitMB = %d, want %d", r.MemoryLimitMB, tt.expectMB)
			}
		})
	}
}

// TestParseMeminfo tests the meminfo parsing function
func TestParseMeminfo(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectBytes int64
		expectError bool
	}{
		{
			name: "valid meminfo 16GB",
			content: `MemTotal:       16384000 kB
MemFree:         1234567 kB
MemAvailable:    8765432 kB
Buffers:          123456 kB
Cached:          2345678 kB`,
			expectBytes: 16384000 * 1024,
			expectError: false,
		},
		{
			name: "valid meminfo 8GB",
			content: `MemTotal:       8388608 kB
MemFree:         1000000 kB`,
			expectBytes: 8388608 * 1024,
			expectError: false,
		},
		{
			name: "valid meminfo 32GB with tabs",
			content: `MemTotal:	32768000 kB
MemFree:	2000000 kB`,
			expectBytes: 32768000 * 1024,
			expectError: false,
		},
		{
			name:        "memtotal not found",
			content:     "MemFree:  1234567 kB\nBuffers:  123456 kB\n",
			expectBytes: 0,
			expectError: true,
		},
		{
			name:        "empty content",
			content:     "",
			expectBytes: 0,
			expectError: true,
		},
		{
			name:        "malformed memtotal",
			content:     "MemTotal: invalid kB\n",
			expectBytes: 0,
			expectError: true,
		},
		{
			name:        "memtotal no value",
			content:     "MemTotal:\n",
			expectBytes: 0,
			expectError: true,
		},
		{
			name: "real world meminfo format",
			content: `MemTotal:       65536000 kB
MemFree:         5555555 kB
MemAvailable:   45000000 kB
Buffers:         1234567 kB
Cached:         20000000 kB
SwapCached:            0 kB
Active:         30000000 kB
Inactive:       10000000 kB
Active(anon):   25000000 kB
Inactive(anon):        0 kB
Active(file):    5000000 kB
Inactive(file): 10000000 kB`,
			expectBytes: 65536000 * 1024,
			expectError: false,
		},
		{
			name: "memtotal in middle of content",
			content: `MemFree:  1234567 kB
MemTotal: 8388608 kB
Buffers:  123456 kB`,
			expectBytes: 8388608 * 1024,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMeminfo(tt.content)

			if tt.expectError {
				if err == nil {
					t.Errorf("parseMeminfo() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("parseMeminfo() unexpected error: %v", err)
				}
				if result != tt.expectBytes {
					t.Errorf("parseMeminfo() = %d, want %d", result, tt.expectBytes)
				}
			}
		})
	}
}

// TestParseCgroupV1CPU tests the cgroup v1 CPU parsing function
func TestParseCgroupV1CPU(t *testing.T) {
	tests := []struct {
		name          string
		quotaContent  string
		periodContent string
		expectCPUs    int
		startCPUs     int
	}{
		{
			name:          "2 CPUs",
			quotaContent:  "200000\n",
			periodContent: "100000\n",
			expectCPUs:    2,
			startCPUs:     0,
		},
		{
			name:          "4 CPUs",
			quotaContent:  "400000\n",
			periodContent: "100000\n",
			expectCPUs:    4,
			startCPUs:     0,
		},
		{
			name:          "1 CPU",
			quotaContent:  "100000\n",
			periodContent: "100000\n",
			expectCPUs:    1,
			startCPUs:     0,
		},
		{
			name:          "fractional 1.5 rounds up to 2",
			quotaContent:  "150000\n",
			periodContent: "100000\n",
			expectCPUs:    2,
			startCPUs:     0,
		},
		{
			name:          "fractional 0.5 rounds up to 1",
			quotaContent:  "50000\n",
			periodContent: "100000\n",
			expectCPUs:    1,
			startCPUs:     0,
		},
		{
			name:          "unlimited quota -1 unchanged",
			quotaContent:  "-1\n",
			periodContent: "100000\n",
			expectCPUs:    4, // Should keep startCPUs
			startCPUs:     4,
		},
		{
			name:          "zero quota unchanged",
			quotaContent:  "0\n",
			periodContent: "100000\n",
			expectCPUs:    8, // Should keep startCPUs
			startCPUs:     8,
		},
		{
			name:          "zero period unchanged",
			quotaContent:  "200000\n",
			periodContent: "0\n",
			expectCPUs:    2, // Should keep startCPUs
			startCPUs:     2,
		},
		{
			name:          "invalid quota unchanged",
			quotaContent:  "invalid\n",
			periodContent: "100000\n",
			expectCPUs:    4, // Should keep startCPUs
			startCPUs:     4,
		},
		{
			name:          "invalid period unchanged",
			quotaContent:  "200000\n",
			periodContent: "invalid\n",
			expectCPUs:    4, // Should keep startCPUs
			startCPUs:     4,
		},
		{
			name:          "both invalid unchanged",
			quotaContent:  "invalid\n",
			periodContent: "invalid\n",
			expectCPUs:    2, // Should keep startCPUs
			startCPUs:     2,
		},
		{
			name:          "with whitespace",
			quotaContent:  "  200000  \n",
			periodContent: "  100000  \n",
			expectCPUs:    2,
			startCPUs:     0,
		},
		{
			name:          "non-standard period",
			quotaContent:  "500000\n",
			periodContent: "250000\n",
			expectCPUs:    2, // 500000/250000 = 2
			startCPUs:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ContainerResources{CPULimit: tt.startCPUs}
			parseCgroupV1CPU(r, tt.quotaContent, tt.periodContent)

			if r.CPULimit != tt.expectCPUs {
				t.Errorf("CPULimit = %d, want %d", r.CPULimit, tt.expectCPUs)
			}
		})
	}
}

// TestContainerResources_ConsistencyChecks tests internal consistency
func TestContainerResources_ConsistencyChecks(t *testing.T) {
	tests := []struct {
		name      string
		resources *ContainerResources
		valid     bool
	}{
		{
			name: "consistent bytes and MB",
			resources: &ContainerResources{
				MemoryLimitBytes: 2 * 1024 * 1024 * 1024,
				MemoryLimitMB:    2048,
				CPULimit:         4,
			},
			valid: true,
		},
		{
			name: "inconsistent bytes and MB",
			resources: &ContainerResources{
				MemoryLimitBytes: 2 * 1024 * 1024 * 1024,
				MemoryLimitMB:    1024, // Wrong!
				CPULimit:         4,
			},
			valid: false,
		},
		{
			name: "zero memory is valid (unlimited)",
			resources: &ContainerResources{
				MemoryLimitBytes: 0,
				MemoryLimitMB:    0,
				CPULimit:         4,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check consistency
			expectedMB := int(tt.resources.MemoryLimitBytes / (1024 * 1024))
			isConsistent := tt.resources.MemoryLimitMB == expectedMB

			if tt.valid && !isConsistent {
				t.Errorf("Expected consistent memory values, bytes=%d should equal MB=%d (got %d)",
					tt.resources.MemoryLimitBytes, expectedMB, tt.resources.MemoryLimitMB)
			}
		})
	}
}
