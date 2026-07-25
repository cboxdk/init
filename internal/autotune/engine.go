package autotune

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
)

// Engine is a database engine cbox-init supervises directly.
type Engine string

const (
	EnginePercona Engine = "percona"
	EngineValkey  Engine = "valkey"
)

// WakeMode says whether the engine is expected to stay resident or to be
// checkpointed while idle and restored on the next connection.
//
// It changes the tuning, and not by a little. A checkpoint captures the
// process's memory, so on the warm path the engine's cache IS the checkpoint:
// every megabyte of buffer pool is a megabyte to dump, to store, and to read
// back before the first query is answered. Tuned purely for throughput, a
// scale-to-zero database wakes slowly and fills the node's disk with snapshots;
// tuned purely for wake latency, a resident database leaves memory the customer
// paid for unused. There is no single right answer, only two.
type WakeMode string

const (
	// WakeResident: the engine runs continuously. Use the memory.
	WakeResident WakeMode = "resident"
	// WakeWarm: the engine is checkpointed when idle. Keep the footprint small
	// enough that waking stays fast.
	WakeWarm WakeMode = "warm"
)

// EngineConfig is the tuning decided for one engine, plus enough context to
// explain it. Nothing here is applied silently: the caller renders it and the
// values are visible in the container's configuration.
type EngineConfig struct {
	Engine   Engine
	WakeMode WakeMode

	// Settings, in the engine's own vocabulary.
	Settings map[string]string

	MemoryLimitMB  int
	MemoryBudgetMB int // what the engine may use after headroom
	ReservedMB     int
	CPUs           int
	Warnings       []string
}

// EngineCalculator derives engine settings from the container's real limits.
type EngineCalculator struct {
	engine    Engine
	wake      WakeMode
	resources *ContainerResources
	logger    *slog.Logger
}

// Reserved headroom. An engine's tunables never account for the whole process:
// connection buffers, sort and join buffers, temporary tables, the allocator's
// own overhead and the supervisor all live outside them. Handing the engine the
// full limit is how a container gets OOM-killed while every setting looks
// correct.
const (
	perconaReservedFraction = 0.25
	perconaReservedFloorMB  = 256
	valkeyReservedFraction  = 0.20
	valkeyReservedFloorMB   = 64
)

// On the warm path the resident cache is capped: past this point the checkpoint
// grows faster than the query time it saves.
const warmBufferPoolCeilingMB = 512

// ParseEngine validates an engine name.
func ParseEngine(name string) (Engine, error) {
	switch Engine(strings.ToLower(strings.TrimSpace(name))) {
	case EnginePercona:
		return EnginePercona, nil
	case EngineValkey:
		return EngineValkey, nil
	default:
		return "", fmt.Errorf("unknown engine %q (want percona or valkey)", name)
	}
}

// ParseWakeMode validates a wake mode, defaulting to resident: a database that
// was never told it may sleep must not be tuned as though it does.
func ParseWakeMode(name string) (WakeMode, error) {
	switch WakeMode(strings.ToLower(strings.TrimSpace(name))) {
	case "", WakeResident:
		return WakeResident, nil
	case WakeWarm:
		return WakeWarm, nil
	default:
		return "", fmt.Errorf("unknown wake mode %q (want resident or warm)", name)
	}
}

// NewEngineCalculator detects the container's limits and prepares to tune.
func NewEngineCalculator(engine Engine, wake WakeMode, logger *slog.Logger) (*EngineCalculator, error) {
	resources, err := DetectContainerResources()
	if err != nil {
		return nil, fmt.Errorf("failed to detect container resources: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &EngineCalculator{engine: engine, wake: wake, resources: resources, logger: logger}, nil
}

// Calculate derives the settings for the engine.
func (c *EngineCalculator) Calculate() (*EngineConfig, error) {
	cfg := &EngineConfig{
		Engine:        c.engine,
		WakeMode:      c.wake,
		Settings:      map[string]string{},
		MemoryLimitMB: c.resources.MemoryLimitMB,
		CPUs:          c.resources.CPULimit,
	}

	if !c.resources.IsContainerized || c.resources.MemoryLimitMB <= 0 {
		// Without a limit there is nothing to size against, and guessing from
		// host memory would be worse than leaving the engine's own defaults in
		// place — the host is not ours to fill.
		cfg.Warnings = append(cfg.Warnings,
			"no container memory limit detected; leaving engine defaults untouched")

		return cfg, nil
	}

	switch c.engine {
	case EnginePercona:
		c.tunePercona(cfg)
	case EngineValkey:
		c.tuneValkey(cfg)
	default:
		return nil, fmt.Errorf("unknown engine %q", c.engine)
	}

	c.log(cfg)

	return cfg, nil
}

func (c *EngineCalculator) tunePercona(cfg *EngineConfig) {
	limit := c.resources.MemoryLimitMB
	cfg.ReservedMB = reserved(limit, perconaReservedFraction, perconaReservedFloorMB)
	cfg.MemoryBudgetMB = limit - cfg.ReservedMB

	pool := cfg.MemoryBudgetMB

	if c.wake == WakeWarm && pool > warmBufferPoolCeilingMB {
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
			"buffer pool capped at %dMB (of %dMB available) because this database is checkpointed while idle: the pool is what a wake has to read back",
			warmBufferPoolCeilingMB, pool))
		pool = warmBufferPoolCeilingMB
	}

	// InnoDB refuses to start with a pool below 5MB, and anything near that is a
	// misconfiguration rather than a small database.
	if pool < 128 {
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
			"only %dMB available for the buffer pool; this container is very likely too small for a database", pool))

		if pool < 32 {
			pool = 32
		}
	}

	cfg.Settings["innodb_buffer_pool_size"] = fmt.Sprintf("%dM", pool)

	// The redo log has to absorb the writes a large pool defers, but a huge log
	// slows crash recovery — and on the warm path it also has to be re-read.
	// A quarter of the pool, bounded, is the conventional balance.
	redo := clamp(pool/4, 48, 2048)
	cfg.Settings["innodb_redo_log_capacity"] = fmt.Sprintf("%dM", redo)

	// Every connection costs memory outside the pool, so connections are sized
	// from the budget as well as from CPU — 12MB per connection is a
	// deliberately pessimistic allowance for sort, join and net buffers.
	byCPU := c.resources.CPULimit * 75
	byMemory := cfg.ReservedMB / 12
	maxConns := clamp(min(byCPU, byMemory), 25, 1000)
	cfg.Settings["max_connections"] = strconv.Itoa(maxConns)

	if byMemory < byCPU {
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
			"max_connections limited to %d by memory rather than the %d cores would allow", maxConns, byCPU))
	}

	// The page cache would otherwise hold a second copy of everything already in
	// the buffer pool — wasted memory when resident, and a larger checkpoint when
	// warm.
	cfg.Settings["innodb_flush_method"] = "O_DIRECT"
}

func (c *EngineCalculator) tuneValkey(cfg *EngineConfig) {
	limit := c.resources.MemoryLimitMB
	cfg.ReservedMB = reserved(limit, valkeyReservedFraction, valkeyReservedFloorMB)
	cfg.MemoryBudgetMB = limit - cfg.ReservedMB

	if cfg.MemoryBudgetMB < 16 {
		cfg.MemoryBudgetMB = 16
		cfg.Warnings = append(cfg.Warnings, "container is very small; maxmemory floored at 16MB")
	}

	// This is the setting that matters most, and it is a correctness one rather
	// than a performance one. Without maxmemory Valkey grows until the cgroup
	// kills the process — the dataset is lost with no chance to react. With it,
	// the engine refuses the write and the client sees an error it can handle.
	cfg.Settings["maxmemory"] = fmt.Sprintf("%dmb", cfg.MemoryBudgetMB)

	// Refuse writes rather than silently discard data. Cortex presents Valkey as
	// a database, and a database that quietly drops rows when full is worse than
	// one that says no. A cache workload should set allkeys-lru explicitly.
	cfg.Settings["maxmemory-policy"] = "noeviction"

	// The dataset is the memory here, so there is no cache to shrink for the
	// warm path — a wake reads back whatever the customer stored. Say so rather
	// than pretending a knob exists.
	if c.wake == WakeWarm {
		cfg.Warnings = append(cfg.Warnings,
			"wake time for Valkey scales with the dataset: the data is the memory, so there is no cache to trade away")
	}
}

// ToEnvVars renders the settings as environment variables, prefixed per engine
// so a container running one cannot be confused by the other's.
func (cfg *EngineConfig) ToEnvVars() map[string]string {
	env := make(map[string]string, len(cfg.Settings))

	prefix := "CBOX_" + strings.ToUpper(string(cfg.Engine)) + "_"

	for key, value := range cfg.Settings {
		env[prefix+strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(key))] = value
	}

	return env
}

// RenderConfig returns the settings as an engine-native configuration fragment,
// sorted so the output is stable and diffable.
func (cfg *EngineConfig) RenderConfig() string {
	keys := make([]string, 0, len(cfg.Settings))
	for key := range cfg.Settings {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	var b strings.Builder

	if cfg.Engine == EnginePercona {
		b.WriteString("[mysqld]\n")
	}

	for _, key := range keys {
		switch cfg.Engine {
		case EnginePercona:
			fmt.Fprintf(&b, "%s = %s\n", key, cfg.Settings[key])
		default:
			fmt.Fprintf(&b, "%s %s\n", key, cfg.Settings[key])
		}
	}

	return b.String()
}

func (c *EngineCalculator) log(cfg *EngineConfig) {
	if c.logger == nil {
		c.logger = slog.Default()
	}

	c.logger.Info("Engine auto-tuned",
		"engine", cfg.Engine,
		"wake_mode", cfg.WakeMode,
		"memory_limit_mb", cfg.MemoryLimitMB,
		"memory_budget_mb", cfg.MemoryBudgetMB,
		"reserved_mb", cfg.ReservedMB,
		"cpus", cfg.CPUs,
		"settings", cfg.Settings,
	)

	for _, warning := range cfg.Warnings {
		c.logger.Warn("Engine auto-tune", "engine", cfg.Engine, "warning", warning)
	}
}

func reserved(limitMB int, fraction float64, floorMB int) int {
	r := int(float64(limitMB) * fraction)
	if r < floorMB {
		r = floorMB
	}

	// Never reserve so much that nothing is left; a tiny container should still
	// start, with a warning, rather than compute a negative budget.
	if r >= limitMB {
		r = limitMB / 2
	}

	return r
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}

	if v > hi {
		return hi
	}

	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
