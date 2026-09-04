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
	strict          bool    // Fail hard when the profile does not fit, instead of clamping
	logger          *slog.Logger
}

// NewCalculator creates a new calculator with detected resources and profile.
//
// strict controls what happens when the container is too small for the profile:
// with strict false (the default) the calculator clamps to the largest worker
// count that fits and warns, so the container still boots and the runtime
// autotuner refines the number; with strict true a misfit is a hard error.
func NewCalculator(profile Profile, memoryThreshold float64, strict bool, logger *slog.Logger) (*Calculator, error) {
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
		strict:          strict,
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

	// Size the pool through one shared memory model. It clamps to the largest
	// worker count that fits (with a warning) rather than refusing, so a container
	// too small for the profile still boots a degraded-but-working pool that the
	// runtime autotuner then refines. In strict mode a misfit is a hard error.
	sized, err := sizeWorkers(c.resources.MemoryLimitMB, c.resources.CPULimit, threshold, c.profile, c.strict)
	if err != nil {
		return nil, err
	}
	maxChildren := sized.maxChildren
	cfg.Warnings = append(cfg.Warnings, sized.warnings...)

	c.logger.Debug("Memory calculation",
		"threshold", fmt.Sprintf("%.1f%%", threshold*100),
		"threshold_source", thresholdSource,
		"total_memory", c.resources.MemoryLimitMB,
		"max_children", maxChildren,
	)

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

// validateConfig ensures the calculated PM relationships are valid. The memory
// fit is owned by sizeWorkers, which clamps to what fits (or errors in strict
// mode), so this no longer refuses on memory: a deliberately over-committed
// one-worker pool on a tiny container has already warned and must still boot.
func (c *Calculator) validateConfig(cfg *PHPFPMConfig) error {
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

// workerSizing is the result of the shared memory model.
type workerSizing struct {
	maxChildren int
	warnings    []string
}

// sizeWorkers is the single memory model the calculator uses. It returns the
// largest safe pm.max_children for the limit rather than refusing: the profile's
// minimum is a preference, not a hard floor that may OOM, so when the container
// cannot fit it the count is clamped to what fits and a warning explains it. Only
// strict mode turns a misfit into an error, naming the profile and the smallest
// limit that runs it.
func sizeWorkers(limitMB, cpus int, threshold float64, p ProfileConfig, strict bool) (workerSizing, error) {
	overhead := p.ReservedMemoryMB + p.OPcacheMemoryMB
	perWorker := p.AvgMemoryPerWorker
	var warnings []string

	// Desired count from the soft (threshold) budget, capped by CPU and profile.
	desired := 0
	if softPool := int(float64(limitMB)*threshold) - overhead; softPool > 0 {
		desired = softPool / perWorker
	}
	if cpuCap := cpus * 4; cpuCap > 0 && desired > cpuCap {
		warnings = append(warnings,
			fmt.Sprintf("memory allows %d workers, limiting to %d for %d CPUs (max 4 per core)", desired, cpuCap, cpus))
		desired = cpuCap
	}
	if p.MaxWorkers > 0 && desired > p.MaxWorkers {
		warnings = append(warnings,
			fmt.Sprintf("calculated %d workers, but the profile limits to %d", desired, p.MaxWorkers))
		desired = p.MaxWorkers
	}
	if desired < p.MinWorkers {
		warnings = append(warnings, fmt.Sprintf(
			"raising to the profile minimum of %d workers (%q; memory and CPU alone suggested %d)",
			p.MinWorkers, p.Name, desired))
		desired = p.MinWorkers
	}

	// Hard ceiling: workers plus overhead must fit the real limit, not just the
	// thresholded budget, or the pool would risk the OOM killer.
	hardMax := 0
	if hardPool := limitMB - overhead; hardPool > 0 {
		hardMax = hardPool / perWorker
	}

	if desired <= hardMax {
		return workerSizing{maxChildren: max(1, desired), warnings: warnings}, nil
	}

	// The profile's floor does not fit this container.
	minLimit := smallestBootableLimit(p)
	if strict {
		return workerSizing{}, fmt.Errorf(
			"profile %q needs at least %dMB for its %d-worker minimum, but the limit is %dMB; "+
				"raise the limit, choose a smaller profile, or set global.autotune_strict: false to clamp and boot",
			p.Name, minLimit, p.MinWorkers, limitMB)
	}
	if hardMax >= 1 {
		warnings = append(warnings, fmt.Sprintf(
			"profile %q wants %d workers (needs %dMB) but the %dMB limit fits %d; clamped to %d, the runtime autotuner will refine it",
			p.Name, p.MinWorkers, minLimit, limitMB, hardMax, hardMax))
		return workerSizing{maxChildren: hardMax, warnings: warnings}, nil
	}
	// Even one worker plus overhead exceeds the limit: boot over-committed with a
	// single worker rather than not at all, and say so loudly.
	warnings = append(warnings, fmt.Sprintf(
		"the %dMB limit is below what profile %q assumes (%dMB per worker plus %dMB overhead); "+
			"booting one worker over-committed, the runtime autotuner will refine it",
		limitMB, p.Name, perWorker, overhead))
	return workerSizing{maxChildren: 1, warnings: warnings}, nil
}

// smallestBootableLimit is the smallest memory limit at which the profile's
// intended minimum worker count fits. The clamp in sizeWorkers triggers on the
// hard limit (workers plus overhead must fit the real memory, not just the
// thresholded budget), so the honest minimum is that same hard floor: the
// threshold shapes the count above the floor, not whether the floor fits. This is
// the number a strict-mode error and a clamp warning report, computed from the
// same model as the sizing so the two can never disagree.
func smallestBootableLimit(p ProfileConfig) int {
	return p.MinWorkers*p.AvgMemoryPerWorker + p.ReservedMemoryMB + p.OPcacheMemoryMB
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
