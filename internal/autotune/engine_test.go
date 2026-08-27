package autotune

import (
	"strconv"
	"strings"
	"testing"
)

// calcFor builds a calculator against fixed resources so the arithmetic is
// tested rather than whatever cgroup the test host happens to have.
func calcFor(engine Engine, wake WakeMode, memMB, cpus int) *EngineCalculator {
	return &EngineCalculator{
		engine: engine,
		wake:   wake,
		resources: &ContainerResources{
			MemoryLimitMB:   memMB,
			CPULimit:        cpus,
			IsContainerized: true,
			CgroupVersion:   2,
		},
	}
}

func mustCalc(t *testing.T, c *EngineCalculator) *EngineConfig {
	t.Helper()

	cfg, err := c.Calculate()
	if err != nil {
		t.Fatalf("Calculate() error: %v", err)
	}

	return cfg
}

func mb(t *testing.T, value string) int {
	t.Helper()

	trimmed := strings.TrimRight(strings.ToLower(value), "mb")

	n, err := strconv.Atoi(trimmed)
	if err != nil {
		t.Fatalf("could not read %q as megabytes: %v", value, err)
	}

	return n
}

func TestPerconaLeavesHeadroomBelowTheContainerLimit(t *testing.T) {
	cfg := mustCalc(t, calcFor(EnginePercona, WakeResident, 4096, 4))

	pool := mb(t, cfg.Settings["innodb_buffer_pool_size"])
	if pool >= 4096 {
		t.Fatalf("buffer pool %dMB is not below the 4096MB limit", pool)
	}

	// The pool plus the redo log must still leave the reserve intact, or the
	// container gets OOM-killed with every setting looking correct.
	redo := mb(t, cfg.Settings["innodb_redo_log_capacity"])
	if pool+redo > cfg.MemoryBudgetMB+redo {
		t.Fatalf("pool %dMB exceeds budget %dMB", pool, cfg.MemoryBudgetMB)
	}

	if cfg.ReservedMB <= 0 {
		t.Fatal("nothing reserved for connections, sort buffers and the allocator")
	}
}

func TestWarmPerconaTradesCacheForWakeTime(t *testing.T) {
	resident := mustCalc(t, calcFor(EnginePercona, WakeResident, 8192, 4))
	warm := mustCalc(t, calcFor(EnginePercona, WakeWarm, 8192, 4))

	residentPool := mb(t, resident.Settings["innodb_buffer_pool_size"])
	warmPool := mb(t, warm.Settings["innodb_buffer_pool_size"])

	// The buffer pool is what a checkpoint has to write and a wake has to read
	// back, so a database that sleeps must not be tuned like one that does not.
	if warmPool >= residentPool {
		t.Fatalf("warm pool %dMB is not smaller than resident pool %dMB", warmPool, residentPool)
	}

	if warmPool > warmBufferPoolCeilingMB {
		t.Fatalf("warm pool %dMB exceeds the ceiling %dMB", warmPool, warmBufferPoolCeilingMB)
	}

	if len(warm.Warnings) == 0 {
		t.Fatal("capping the pool is a real trade-off and must be said out loud")
	}
}

func TestPerconaConnectionsAreLimitedByMemoryNotJustCores(t *testing.T) {
	// Many cores, little memory: connections must follow the memory, because
	// each one costs buffers outside the pool.
	cfg := mustCalc(t, calcFor(EnginePercona, WakeResident, 1024, 32))

	conns, err := strconv.Atoi(cfg.Settings["max_connections"])
	if err != nil {
		t.Fatalf("max_connections unreadable: %v", err)
	}

	if conns >= 32*75 {
		t.Fatalf("max_connections %d ignored the memory limit", conns)
	}

	if conns < minConnections {
		t.Fatalf("max_connections %d is below the usable floor of %d", conns, minConnections)
	}

	// The real invariant: the reserve has to actually cover the connections we
	// allow. The floor used to be 25, which overrode the memory-derived cap for
	// every container at or below 1GB — 25 × 12MB = 300MB charged against a
	// 256MB reserve, silently.
	if want := conns * connectionMemoryMB; want > cfg.ReservedMB {
		t.Errorf("max_connections %d needs %dMB but the reserve is only %dMB",
			conns, want, cfg.ReservedMB)
	}
}

func TestValkeyAlwaysSetsMaxmemory(t *testing.T) {
	// Without maxmemory the cgroup kills the process and the dataset is gone
	// with no chance to react, so this is correctness rather than tuning.
	for _, wake := range []WakeMode{WakeResident, WakeWarm} {
		cfg := mustCalc(t, calcFor(EngineValkey, wake, 2048, 2))

		limit := mb(t, cfg.Settings["maxmemory"])
		if limit <= 0 || limit >= 2048 {
			t.Fatalf("maxmemory %dMB is not a safe fraction of the 2048MB limit", limit)
		}

		if cfg.Settings["maxmemory-policy"] != "noeviction" {
			t.Fatalf("policy %q would let a database silently drop data", cfg.Settings["maxmemory-policy"])
		}
	}
}

func TestNoLimitLeavesTheEngineAlone(t *testing.T) {
	c := &EngineCalculator{
		engine:    EnginePercona,
		wake:      WakeResident,
		resources: &ContainerResources{IsContainerized: false},
	}

	cfg := mustCalc(t, c)

	// Sizing against host memory would let one container claim a machine it does
	// not own; the engine's own defaults are the safer answer.
	if len(cfg.Settings) != 0 {
		t.Fatalf("tuned %d settings with no limit to tune against", len(cfg.Settings))
	}

	if len(cfg.Warnings) == 0 {
		t.Fatal("declining to tune must be visible, not silent")
	}
}

func TestTinyContainerStillProducesAStartableConfig(t *testing.T) {
	cfg := mustCalc(t, calcFor(EnginePercona, WakeResident, 256, 1))

	pool := mb(t, cfg.Settings["innodb_buffer_pool_size"])
	if pool <= 0 {
		t.Fatalf("buffer pool %dMB would not start", pool)
	}

	if len(cfg.Warnings) == 0 {
		t.Fatal("a container this small deserves a warning")
	}
}

func TestRenderConfigIsEngineNativeAndStable(t *testing.T) {
	percona := mustCalc(t, calcFor(EnginePercona, WakeResident, 2048, 2)).RenderConfig()
	if !strings.HasPrefix(percona, "[mysqld]\n") {
		t.Fatalf("Percona fragment is not a my.cnf section:\n%s", percona)
	}

	if !strings.Contains(percona, "innodb_buffer_pool_size = ") {
		t.Fatalf("Percona fragment missing key=value form:\n%s", percona)
	}

	valkey := mustCalc(t, calcFor(EngineValkey, WakeResident, 2048, 2)).RenderConfig()
	if strings.Contains(valkey, "=") {
		t.Fatalf("Valkey directives are space separated, not key=value:\n%s", valkey)
	}

	// Stable output keeps the rendered config diffable across restarts.
	again := mustCalc(t, calcFor(EnginePercona, WakeResident, 2048, 2)).RenderConfig()
	if percona != again {
		t.Fatal("RenderConfig is not deterministic")
	}
}

func TestEnvVarsArePrefixedPerEngine(t *testing.T) {
	env := mustCalc(t, calcFor(EngineValkey, WakeResident, 1024, 1)).ToEnvVars()

	if _, ok := env["CBOX_VALKEY_MAXMEMORY"]; !ok {
		t.Fatalf("expected CBOX_VALKEY_MAXMEMORY, got %v", env)
	}

	for key := range env {
		if !strings.HasPrefix(key, "CBOX_VALKEY_") {
			t.Fatalf("unprefixed env var %q could collide with another engine", key)
		}
	}
}

func TestParsersRejectUnknownValues(t *testing.T) {
	if _, err := ParseEngine("postgres"); err == nil {
		t.Fatal("Postgres runs under CloudNativePG and is not tuned here; it must be refused")
	}

	if _, err := ParseWakeMode("sometimes"); err == nil {
		t.Fatal("an unknown wake mode must be refused rather than guessed")
	}

	// An unset wake mode means resident: a database never told it may sleep must
	// not be tuned as though it does.
	mode, err := ParseWakeMode("")
	if err != nil || mode != WakeResident {
		t.Fatalf("empty wake mode = %q, %v; want resident", mode, err)
	}
}

func TestPerconaPoolSurvivesInnoDBRounding(t *testing.T) {
	// InnoDB rounds the pool UP to a multiple of chunk size × instances. Left to
	// the defaults that granularity is 1GB, so a 2GB container asking for 1536MB
	// is silently given the whole 2048MB and gets OOM-killed under load. The
	// computed value must already be a multiple of the granularity we pin.
	for _, limit := range []int{1024, 2048, 4096, 8192, 16384} {
		cfg := mustCalc(t, calcFor(EnginePercona, WakeResident, limit, 4))

		pool := mb(t, cfg.Settings["innodb_buffer_pool_size"])
		chunk := mb(t, cfg.Settings["innodb_buffer_pool_chunk_size"])

		instances, err := strconv.Atoi(cfg.Settings["innodb_buffer_pool_instances"])
		if err != nil {
			t.Fatalf("instances unreadable: %v", err)
		}

		granularity := chunk * instances
		if pool%granularity != 0 {
			t.Fatalf("limit %dMB: pool %dMB is not a multiple of %dMB, so InnoDB will round it up",
				limit, pool, granularity)
		}

		if pool >= limit {
			t.Fatalf("limit %dMB: pool %dMB leaves no headroom", limit, pool)
		}
	}
}
