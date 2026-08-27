package autotune

import "fmt"

// Profile represents an application workload profile for PHP-FPM tuning
type Profile string

const (
	ProfileDev    Profile = "dev"    // Development: minimal resources, fast startup
	ProfileLight  Profile = "light"  // Light: small apps, low traffic (1-10 req/s)
	ProfileMedium Profile = "medium" // Medium: standard production (10-50 req/s)
	ProfileHeavy  Profile = "heavy"  // Heavy: high traffic, many workers (50-200 req/s)
	ProfileBursty Profile = "bursty" // Bursty: handle traffic spikes with dynamic scaling
)

// ProfileConfig defines the characteristics of each application profile
type ProfileConfig struct {
	Name                string
	Description         string
	ProcessManagerType  string  // static, dynamic, ondemand
	AvgMemoryPerWorker  int     // MB - average memory per PHP-FPM worker (excluding shared OPcache)
	OPcacheMemoryMB     int     // MB - shared OPcache memory pool (added to reserved)
	MinWorkers          int     // Minimum workers to maintain
	MaxWorkers          int     // Maximum workers (0 = auto-calculate)
	SpareMinRatio       float64 // Min spare servers as ratio of max_children
	SpareMaxRatio       float64 // Max spare servers as ratio of max_children
	StartServersRatio   float64 // Start servers as ratio of max_children
	MaxRequestsPerChild int     // Anti-memory-leak: restart worker after N requests
	MaxMemoryUsage      float64 // Max % of available memory to use (safety)
	ReservedMemoryMB    int     // Memory reserved for system/Nginx/etc (excluding OPcache)
}

// Profiles defines safe, production-tested configurations for each profile
// Memory estimates based on real Laravel applications WITH OPcache enabled:
//
// OPcache REDUCES per-worker memory by storing compiled app code in shared memory!
// - WITHOUT OPcache: Worker = runtime + app code + request = ~80MB
// - WITH OPcache: Worker = runtime + request = ~40MB, app code in shared OPcache
//
// Memory breakdown WITH OPcache:
// - Worker memory: PHP runtime (~25-32MB) + request data (~4-8MB) + overhead (~3-12MB)
// - OPcache (shared): Compiled app opcodes (64-256MB, depending on app size)
var Profiles = map[Profile]ProfileConfig{
	// Development: Fast startup, minimal resource usage, debugging friendly
	ProfileDev: {
		Name:                "Development",
		Description:         "Minimal resources for local development (2 workers, fast startup)",
		ProcessManagerType:  "static", // Simple for debugging
		AvgMemoryPerWorker:  32,       // Runtime ~25MB + request ~4MB + overhead ~3MB (app code in OPcache)
		OPcacheMemoryMB:     64,       // Compiled opcodes for small Laravel app
		MinWorkers:          2,
		MaxWorkers:          2,
		SpareMinRatio:       0,
		SpareMaxRatio:       0,
		StartServersRatio:   1.0,
		MaxRequestsPerChild: 100, // Restart frequently to catch leaks
		MaxMemoryUsage:      0.5, // Use only 50% to leave room for IDE, etc
		ReservedMemoryMB:    64,  // Minimal system overhead
	},

	// Light: Small applications, low traffic, cost-optimized
	ProfileLight: {
		Name:                "Light Production",
		Description:         "Small apps, low traffic 1-10 req/s (~36MB per worker)",
		ProcessManagerType:  "dynamic",
		AvgMemoryPerWorker:  36, // Runtime ~25MB + request ~6MB + overhead ~5MB (app code in OPcache)
		OPcacheMemoryMB:     96, // Compiled opcodes for small Laravel app with some packages
		MinWorkers:          2,
		MaxWorkers:          0, // Auto-calculate
		SpareMinRatio:       0.25,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.25,
		MaxRequestsPerChild: 500,
		MaxMemoryUsage:      0.7,
		ReservedMemoryMB:    128, // Nginx + system
	},

	// Medium: Standard production workloads, balanced performance
	ProfileMedium: {
		Name:                "Medium Production",
		Description:         "Standard production 10-50 req/s (~42MB per worker)",
		ProcessManagerType:  "dynamic",
		AvgMemoryPerWorker:  42,  // Runtime ~28MB + request ~6MB + overhead ~8MB (app code in OPcache)
		OPcacheMemoryMB:     128, // Compiled opcodes for standard Laravel with packages
		MinWorkers:          4,
		MaxWorkers:          0, // Auto-calculate based on resources
		SpareMinRatio:       0.25,
		SpareMaxRatio:       0.5,
		StartServersRatio:   0.33,
		MaxRequestsPerChild: 1000,
		MaxMemoryUsage:      0.75,
		ReservedMemoryMB:    192, // Nginx + Redis/MySQL clients + system
	},

	// Heavy: High traffic, large apps with many dependencies
	ProfileHeavy: {
		Name:                "Heavy Production",
		Description:         "High traffic 50-200 req/s, large apps (~52MB per worker)",
		ProcessManagerType:  "dynamic",
		AvgMemoryPerWorker:  52,  // Runtime ~32MB + request ~8MB + overhead ~12MB (app code in OPcache)
		OPcacheMemoryMB:     256, // Compiled opcodes for large Laravel with many packages
		MinWorkers:          8,
		MaxWorkers:          0, // Auto-calculate
		SpareMinRatio:       0.2,
		SpareMaxRatio:       0.4,
		StartServersRatio:   0.5,
		MaxRequestsPerChild: 2000,
		MaxMemoryUsage:      0.8,
		ReservedMemoryMB:    384, // More headroom for connections, cache
	},

	// Bursty: Handle traffic spikes with aggressive spare server settings
	ProfileBursty: {
		Name:                "Bursty Traffic",
		Description:         "Handle traffic spikes with dynamic scaling (~44MB per worker)",
		ProcessManagerType:  "dynamic",
		AvgMemoryPerWorker:  44,  // Runtime ~30MB + request ~6MB + overhead ~8MB (app code in OPcache)
		OPcacheMemoryMB:     128, // Compiled opcodes for standard Laravel (same as medium)
		MinWorkers:          4,
		MaxWorkers:          0,   // Auto-calculate
		SpareMinRatio:       0.4, // Keep more workers ready
		SpareMaxRatio:       0.7, // Higher ceiling for spikes
		StartServersRatio:   0.5, // Start with more workers
		MaxRequestsPerChild: 1000,
		MaxMemoryUsage:      0.75,
		ReservedMemoryMB:    192,
	},
}

// Validate ensures the profile exists
func (p Profile) Validate() error {
	if _, exists := Profiles[p]; !exists {
		return fmt.Errorf("invalid profile: %s (valid: dev, light, medium, heavy, bursty)", p)
	}
	return nil
}

// GetConfig returns the configuration for this profile.
//
// Profiles is an exported, mutable package map, so a downstream consumer can
// register its own. A profile with AvgMemoryPerWorker of 0 divides by zero in
// the calculator — inside PID 1, that is a panic that takes the container with
// it. The invariant is cheap to check here, once, rather than at every use.
func (p Profile) GetConfig() (ProfileConfig, error) {
	if err := p.Validate(); err != nil {
		return ProfileConfig{}, err
	}

	cfg := Profiles[p]
	if cfg.AvgMemoryPerWorker <= 0 {
		return ProfileConfig{}, fmt.Errorf("profile %s has a non-positive avg_memory_per_worker (%dMB): "+
			"worker sizing divides by it", p, cfg.AvgMemoryPerWorker)
	}
	if cfg.MaxMemoryUsage <= 0 {
		return ProfileConfig{}, fmt.Errorf("profile %s has a non-positive max_memory_usage (%v)", p, cfg.MaxMemoryUsage)
	}

	return cfg, nil
}

// String returns the string representation of the Profile
func (p Profile) String() string {
	return string(p)
}
