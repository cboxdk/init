package autotune

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ContainerResources represents detected container resource limits
type ContainerResources struct {
	MemoryLimitBytes int64 // Total memory limit in bytes (0 = unlimited)
	MemoryLimitMB    int   // Memory limit in MB for convenience
	CPULimit         int   // Number of CPU cores (including fractional via quota/period)
	IsContainerized  bool  // True if running in a container with limits
	CgroupVersion    int   // 1 or 2
}

// DetectContainerResources reads cgroup v1/v2 to determine container limits
// Returns host resources if not containerized or limits not set
func DetectContainerResources() (*ContainerResources, error) {
	resources := &ContainerResources{
		CPULimit: runtime.NumCPU(), // Default to host CPUs
	}

	// Try cgroup v2 first (newer, unified hierarchy)
	if cgroupV2Exists() {
		if err := detectCgroupV2Resources(resources); err == nil {
			resources.CgroupVersion = 2
			resources.IsContainerized = true
			return resources, nil
		}
	}

	// Fallback to cgroup v1
	if cgroupV1Exists() {
		if err := detectCgroupV1Resources(resources); err == nil {
			resources.CgroupVersion = 1
			resources.IsContainerized = true
			return resources, nil
		}
	}

	// Not containerized or limits not detected - use host resources
	resources.IsContainerized = false
	if memTotal, err := getHostMemory(); err == nil {
		resources.MemoryLimitBytes = memTotal
		resources.MemoryLimitMB = int(memTotal / (1024 * 1024))
	}

	return resources, nil
}

// cgroupV2Exists checks if cgroup v2 is available
func cgroupV2Exists() bool {
	_, err := os.Stat("/sys/fs/cgroup/cgroup.controllers")
	return err == nil
}

// cgroupV1Exists checks if cgroup v1 is available
func cgroupV1Exists() bool {
	_, err := os.Stat("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	return err == nil
}

// detectCgroupV2Resources reads cgroup v2 unified hierarchy
func detectCgroupV2Resources(r *ContainerResources) error {
	// Read memory limit from cgroup v2
	memMax, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err == nil {
		parseCgroupV2Memory(r, string(memMax))
	}

	// Read CPU quota and period from cgroup v2
	cpuMax, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err == nil {
		parseCgroupV2CPU(r, string(cpuMax))
	}

	// If memory limit found, consider it a success
	if r.MemoryLimitBytes > 0 {
		return nil
	}

	return fmt.Errorf("cgroup v2 limits not found")
}

// parseCgroupV2Memory parses cgroup v2 memory.max content
func parseCgroupV2Memory(r *ContainerResources, content string) {
	memStr := strings.TrimSpace(content)
	if memStr != "max" {
		if memLimit, err := strconv.ParseInt(memStr, 10, 64); err == nil {
			// Same sanity bound as the v1 parser below. Normal runtimes write
			// "max" when there is no limit, but some write the raw sentinel
			// (9223372036854771712), which parsed cleanly into a limit of ~8.8
			// exabytes — and every derived setting scaled off it, producing an
			// innodb_buffer_pool_size of 6597069766656M.
			if memLimit > 0 && memLimit < (1<<50) {
				r.MemoryLimitBytes = memLimit
				r.MemoryLimitMB = int(memLimit / (1024 * 1024))
			}
		}
	}
}

// parseCgroupV2CPU parses cgroup v2 cpu.max content
func parseCgroupV2CPU(r *ContainerResources, content string) {
	parts := strings.Fields(strings.TrimSpace(content))
	if len(parts) == 2 && parts[0] != "max" {
		quota, err1 := strconv.ParseInt(parts[0], 10, 64)
		period, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 == nil && err2 == nil && period > 0 {
			// CPU limit = quota / period (fractional CPUs supported)
			cpus := int((quota + period - 1) / period) // Round up
			if cpus > 0 {
				r.CPULimit = cpus
			}
		}
	}
}

// detectCgroupV1Resources reads cgroup v1 hierarchy
func detectCgroupV1Resources(r *ContainerResources) error {
	// Read memory limit from cgroup v1
	memLimit, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	if err == nil {
		parseCgroupV1Memory(r, string(memLimit))
	}

	// Read CPU quota from cgroup v1
	quota, err := os.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	if err == nil {
		// Read CPU period
		period, err := os.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
		if err == nil {
			parseCgroupV1CPU(r, string(quota), string(period))
		}
	}

	// If memory limit found, consider it a success
	if r.MemoryLimitBytes > 0 {
		return nil
	}

	return fmt.Errorf("cgroup v1 limits not found")
}

// parseCgroupV1Memory parses cgroup v1 memory.limit_in_bytes content
func parseCgroupV1Memory(r *ContainerResources, content string) {
	memStr := strings.TrimSpace(content)
	if limit, err := strconv.ParseInt(memStr, 10, 64); err == nil {
		// cgroup v1 sets very large values when unlimited
		// Also reject negative values (invalid)
		if limit > 0 && limit < (1<<50) { // Positive and less than ~1PB = realistic limit
			r.MemoryLimitBytes = limit
			r.MemoryLimitMB = int(limit / (1024 * 1024))
		}
	}
}

// parseCgroupV1CPU parses cgroup v1 cpu.cfs_quota_us and cpu.cfs_period_us content
func parseCgroupV1CPU(r *ContainerResources, quotaContent, periodContent string) {
	quotaStr := strings.TrimSpace(quotaContent)
	periodStr := strings.TrimSpace(periodContent)

	quotaVal, err1 := strconv.ParseInt(quotaStr, 10, 64)
	periodVal, err2 := strconv.ParseInt(periodStr, 10, 64)

	if err1 == nil && err2 == nil && quotaVal > 0 && periodVal > 0 {
		// CPU limit = quota / period
		cpus := int((quotaVal + periodVal - 1) / periodVal) // Round up
		if cpus > 0 {
			r.CPULimit = cpus
		}
	}
}

// getHostMemory reads total system memory from /proc/meminfo
func getHostMemory() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}

	return parseMeminfo(string(data))
}

// parseMeminfo parses /proc/meminfo content and extracts MemTotal
func parseMeminfo(content string) (int64, error) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb * 1024, nil // Convert KB to bytes
				}
			}
		}
	}

	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

// String returns a human-readable representation of the resources
func (r *ContainerResources) String() string {
	source := "host"
	if r.IsContainerized {
		source = fmt.Sprintf("cgroup v%d", r.CgroupVersion)
	}

	memStr := "unlimited"
	if r.MemoryLimitMB > 0 {
		memStr = fmt.Sprintf("%dMB", r.MemoryLimitMB)
	}

	return fmt.Sprintf("Resources[%s]: Memory=%s, CPUs=%d", source, memStr, r.CPULimit)
}
