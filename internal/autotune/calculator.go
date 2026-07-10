package autotune

import (
	"fmt"
	"log/slog"
	"math"
)

// PHPFPMConfig represents calculated PHP-FPM pool configuration
type PHPFPMConfig struct {
	// Pool settings
	ProcessManager string // static, dynamic, ondemand
	MaxChildren    int    // pm.max_children
	StartServers   int    // pm.start_servers (dynamic only)
	MinSpare       int    // pm.min_spare_servers (dynamic only)
	MaxSpare       int    // pm.max_spare_servers (dynamic only)
	MaxRequests    int    // pm.max_requests

	// Metadata
	Profile         Profile
	MemoryAllocated int // MB allocated to PHP-FPM workers
	MemoryOPcache   int // MB allocated to shared OPcache
	MemoryReserved  int // MB reserved for system/Nginx
	MemoryTotal     int // MB total available
	CPUs            int
	Warnings        []string
}

// Calculator computes optimal PHP-FPM settings based on profile and resources
type Calculator struct {
	resources       *ContainerResources
	profile         ProfileConfig
	memoryThreshold float64 // Override for profile.MaxMemoryUsage (0.0 = use profile default)
	logger          *slog.Logger
}

// NewCalculator creates a new calculator with detected resources and profile
func NewCalculator(profile Profile, memoryThreshold float64, logger *slog.Logger) (*Calculator, error) {
	profileConfig, err := profile.GetConfig()
	if err != nil {
		return nil, err
	}

	resources, err := DetectContainerResources()
	if err != nil {
		return nil, fmt.Errorf("failed to detect container resources: %w", err)
	}

	return &Calculator{
		resources:       resources,
		profile:         profileConfig,
		memoryThreshold: memoryThreshold,
		logger:          logger,
	}, nil
}

// Calculate computes optimal PHP-FPM configuration with safety validations
func (c *Calculator) Calculate() (*PHPFPMConfig, error) {
	cfg := &PHPFPMConfig{
		Profile:        Profile(c.profile.Name),
		MemoryTotal:    c.resources.MemoryLimitMB,
		MemoryOPcache:  c.profile.OPcacheMemoryMB,
		MemoryReserved: c.profile.ReservedMemoryMB,
		CPUs:           c.resources.CPULimit,
		Warnings:       []string{},
	}

	// WARNING: Auto-tuning without container limits uses host resources
	if !c.resources.IsContainerized {
		cfg.Warnings = append(cfg.Warnings,
			fmt.Sprintf("Auto-tuning WITHOUT container limits - using host resources (%dMB memory, %d CPUs)",
				c.resources.MemoryLimitMB, c.resources.CPULimit))
		cfg.Warnings = append(cfg.Warnings,
			"RECOMMENDED: Set container limits for accurate auto-tuning")
		cfg.Warnings = append(cfg.Warnings,
			"  Docker: 'docker run -m 2G --cpus 2'")
		cfg.Warnings = append(cfg.Warnings,
			"  Docker Compose: deploy.resources.limits.memory and cpus")
		cfg.Warnings = append(cfg.Warnings,
			"  Kubernetes: resources.limits.memory and cpu")
	}

	// Validate minimum memory requirement
	minRequired := c.profile.ReservedMemoryMB + c.profile.OPcacheMemoryMB + c.profile.AvgMemoryPerWorker
	if c.resources.MemoryLimitMB < minRequired {
		return nil, fmt.Errorf("insufficient memory: %dMB (minimum %dMB required: %dMB reserved + %dMB OPcache + %dMB per worker)",
			c.resources.MemoryLimitMB, minRequired, c.profile.ReservedMemoryMB, c.profile.OPcacheMemoryMB, c.profile.AvgMemoryPerWorker)
	}

	// Determine memory threshold (allow override of profile default)
	threshold := c.profile.MaxMemoryUsage
	thresholdSource := "profile"
	if c.memoryThreshold > 0 {
		threshold = c.memoryThreshold
		thresholdSource = "override"

		// WARNING: Oversubscription (>1.0) is dangerous but allowed for experts
		if threshold > 1.0 {
			cfg.Warnings = append(cfg.Warnings,
				fmt.Sprintf("DANGER: Memory threshold %.1f%% (>100%%) allows OVERSUBSCRIPTION - risk of OOM kills!", threshold*100))
			cfg.Warnings = append(cfg.Warnings,
				"This is an expert setting. Ensure you understand the risks.")
		}

		// WARNING: Very low threshold might be too conservative
		if threshold < 0.3 {
			cfg.Warnings = append(cfg.Warnings,
				fmt.Sprintf("WARNING: Memory threshold %.1f%% is very conservative - may waste resources", threshold*100))
		}
	}

	// Calculate available memory for PHP-FPM workers
	// Formula: (Total × Threshold%) - Reserved - OPcache (shared) = Worker Memory Pool
	availableMemory := int(float64(c.resources.MemoryLimitMB) * threshold)
	totalReserved := c.profile.ReservedMemoryMB + c.profile.OPcacheMemoryMB
	workerMemory := availableMemory - totalReserved

	c.logger.Debug("Memory calculation",
		"threshold", fmt.Sprintf("%.1f%%", threshold*100),
		"threshold_source", thresholdSource,
		"total_memory", c.resources.MemoryLimitMB,
		"available_after_threshold", availableMemory,
		"reserved", totalReserved,
		"worker_pool", workerMemory,
	)

	if workerMemory < c.profile.AvgMemoryPerWorker {
		return nil, fmt.Errorf("insufficient memory for workers: %dMB available after reserving %dMB (system: %dMB + OPcache: %dMB), need at least %dMB per worker",
			workerMemory, totalReserved, c.profile.ReservedMemoryMB, c.profile.OPcacheMemoryMB, c.profile.AvgMemoryPerWorker)
	}

	// Calculate max_children based on available memory
	maxChildren := workerMemory / c.profile.AvgMemoryPerWorker

	// Apply CPU-based limit: max 4 workers per CPU core (industry standard)
	cpuBasedMax := c.resources.CPULimit * 4
	if maxChildren > cpuBasedMax {
		cfg.Warnings = append(cfg.Warnings,
			fmt.Sprintf("Memory allows %d workers, but limiting to %d based on %d CPUs (max 4 per core)",
				maxChildren, cpuBasedMax, c.resources.CPULimit))
		maxChildren = cpuBasedMax
	}

	// Apply profile-specific max if set
	if c.profile.MaxWorkers > 0 && maxChildren > c.profile.MaxWorkers {
		cfg.Warnings = append(cfg.Warnings,
			fmt.Sprintf("Calculated %d workers, but profile limits to %d", maxChildren, c.profile.MaxWorkers))
		maxChildren = c.profile.MaxWorkers
	}

	// Enforce profile minimum
	if maxChildren < c.profile.MinWorkers {
		maxChildren = c.profile.MinWorkers
		cfg.Warnings = append(cfg.Warnings,
			fmt.Sprintf("Increasing to profile minimum: %d workers", maxChildren))
	}

	// Absolute minimum safety check
	if maxChildren < 1 {
		maxChildren = 1
		cfg.Warnings = append(cfg.Warnings, "Using absolute minimum: 1 worker")
	}

	cfg.MaxChildren = maxChildren
	cfg.ProcessManager = c.profile.ProcessManagerType
	cfg.MaxRequests = c.profile.MaxRequestsPerChild
	cfg.MemoryAllocated = maxChildren * c.profile.AvgMemoryPerWorker

	// Calculate PM settings per process-manager mode
	switch cfg.ProcessManager {
	case "dynamic":
		cfg.StartServers = int(math.Ceil(float64(maxChildren) * c.profile.StartServersRatio))
		cfg.MinSpare = int(math.Ceil(float64(maxChildren) * c.profile.SpareMinRatio))
		cfg.MaxSpare = int(math.Ceil(float64(maxChildren) * c.profile.SpareMaxRatio))

		// Validate relationships: min_spare <= start_servers <= max_spare <= max_children
		if cfg.MinSpare < 1 {
			cfg.MinSpare = 1
		}
		if cfg.StartServers < cfg.MinSpare {
			cfg.StartServers = cfg.MinSpare
		}
		if cfg.MaxSpare < cfg.StartServers {
			cfg.MaxSpare = cfg.StartServers
		}
		if cfg.MaxSpare > cfg.MaxChildren {
			cfg.MaxSpare = cfg.MaxChildren
		}
	case "static":
		// Static mode: always maintain max_children workers
		cfg.StartServers = cfg.MaxChildren
	}

	// Validate final configuration
	if err := c.validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	c.logCalculation(cfg)
	return cfg, nil
}

// validateConfig ensures the calculated configuration is safe and valid
func (c *Calculator) validateConfig(cfg *PHPFPMConfig) error {
	// Validate memory won't exceed container limit
	// Total = Workers + OPcache (shared) + Reserved (Nginx/system)
	totalMemory := cfg.MemoryAllocated + cfg.MemoryOPcache + cfg.MemoryReserved
	if totalMemory > cfg.MemoryTotal {
		return fmt.Errorf("configuration would use %dMB (workers: %dMB + OPcache: %dMB + reserved: %dMB) but only %dMB available",
			totalMemory, cfg.MemoryAllocated, cfg.MemoryOPcache, cfg.MemoryReserved, cfg.MemoryTotal)
	}

	// Validate PM settings
	if cfg.ProcessManager == "dynamic" {
		if cfg.MinSpare > cfg.MaxChildren {
			return fmt.Errorf("min_spare_servers (%d) > max_children (%d)", cfg.MinSpare, cfg.MaxChildren)
		}
		if cfg.MaxSpare > cfg.MaxChildren {
			return fmt.Errorf("max_spare_servers (%d) > max_children (%d)", cfg.MaxSpare, cfg.MaxChildren)
		}
		if cfg.StartServers > cfg.MaxChildren {
			return fmt.Errorf("start_servers (%d) > max_children (%d)", cfg.StartServers, cfg.MaxChildren)
		}
		if cfg.MinSpare > cfg.MaxSpare {
			return fmt.Errorf("min_spare_servers (%d) > max_spare_servers (%d)", cfg.MinSpare, cfg.MaxSpare)
		}
	}

	return nil
}

// logCalculation logs the calculated configuration details
func (c *Calculator) logCalculation(cfg *PHPFPMConfig) {
	c.logger.Info("PHP-FPM auto-tuning calculated",
		"profile", c.profile.Name,
		"pm", cfg.ProcessManager,
		"max_children", cfg.MaxChildren,
		"start_servers", cfg.StartServers,
		"min_spare", cfg.MinSpare,
		"max_spare", cfg.MaxSpare,
		"max_requests", cfg.MaxRequests,
		"memory_workers", fmt.Sprintf("%dMB", cfg.MemoryAllocated),
		"memory_opcache", fmt.Sprintf("%dMB (shared)", cfg.MemoryOPcache),
		"memory_reserved", fmt.Sprintf("%dMB", cfg.MemoryReserved),
		"memory_total", fmt.Sprintf("%dMB", cfg.MemoryTotal),
		"cpus", cfg.CPUs,
		"avg_memory_per_worker", fmt.Sprintf("%dMB", c.profile.AvgMemoryPerWorker),
		"warnings", len(cfg.Warnings),
	)

	for _, warning := range cfg.Warnings {
		c.logger.Warn("Auto-tuning warning", "message", warning)
	}
}

// ToEnvVars converts the configuration to environment variables for PHP-FPM
func (cfg *PHPFPMConfig) ToEnvVars() map[string]string {
	env := map[string]string{
		"PHP_FPM_PM":           cfg.ProcessManager,
		"PHP_FPM_MAX_CHILDREN": fmt.Sprintf("%d", cfg.MaxChildren),
		"PHP_FPM_MAX_REQUESTS": fmt.Sprintf("%d", cfg.MaxRequests),
	}

	if cfg.ProcessManager == "dynamic" {
		env["PHP_FPM_START_SERVERS"] = fmt.Sprintf("%d", cfg.StartServers)
		env["PHP_FPM_MIN_SPARE"] = fmt.Sprintf("%d", cfg.MinSpare)
		env["PHP_FPM_MAX_SPARE"] = fmt.Sprintf("%d", cfg.MaxSpare)
	}

	return env
}

// String returns a human-readable representation of the configuration
func (cfg *PHPFPMConfig) String() string {
	s := fmt.Sprintf("PHP-FPM Configuration (%s profile):\n", cfg.Profile)
	s += fmt.Sprintf("  pm = %s\n", cfg.ProcessManager)
	s += fmt.Sprintf("  pm.max_children = %d\n", cfg.MaxChildren)

	if cfg.ProcessManager == "dynamic" {
		s += fmt.Sprintf("  pm.start_servers = %d\n", cfg.StartServers)
		s += fmt.Sprintf("  pm.min_spare_servers = %d\n", cfg.MinSpare)
		s += fmt.Sprintf("  pm.max_spare_servers = %d\n", cfg.MaxSpare)
	}

	s += fmt.Sprintf("  pm.max_requests = %d\n", cfg.MaxRequests)
	s += "\nMemory Breakdown:\n"
	s += fmt.Sprintf("  Total Container Memory: %dMB\n", cfg.MemoryTotal)
	s += fmt.Sprintf("  Workers (%d × worker memory): %dMB\n", cfg.MaxChildren, cfg.MemoryAllocated)
	s += fmt.Sprintf("  OPcache (shared): %dMB\n", cfg.MemoryOPcache)
	s += fmt.Sprintf("  Reserved (Nginx/system): %dMB\n", cfg.MemoryReserved)
	s += fmt.Sprintf("  Total Used: %dMB (%.1f%%)\n",
		cfg.MemoryAllocated+cfg.MemoryOPcache+cfg.MemoryReserved,
		float64(cfg.MemoryAllocated+cfg.MemoryOPcache+cfg.MemoryReserved)/float64(cfg.MemoryTotal)*100)
	s += fmt.Sprintf("  CPUs: %d\n", cfg.CPUs)

	if len(cfg.Warnings) > 0 {
		s += "\nWarnings:\n"
		for _, w := range cfg.Warnings {
			s += fmt.Sprintf("  - %s\n", w)
		}
	}

	return s
}
