package autotune

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
)

func calcAt(limitMB, cpus int) *EngineCalculator {
	return &EngineCalculator{
		engine: EnginePercona,
		wake:   WakeResident,
		logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
		resources: &ContainerResources{
			IsContainerized: true, MemoryLimitMB: limitMB, CPULimit: cpus,
		},
	}
}

// TestBufferPoolNeverExceedsTheContainerLimit: innodb_buffer_pool_chunk_size was
// pinned at 128MB, so in a small container the rounding granularity exceeded the
// whole budget, rounding down produced zero, and the floor then raised the pool
// back to a full 128MB chunk — 128MB of pool inside a 128MB container that had
// already reserved 64MB for the server. The written fragment guaranteed the OOM
// kill the reserve exists to prevent.
func TestBufferPoolNeverExceedsTheContainerLimit(t *testing.T) {
	for _, limit := range []int{64, 128, 200, 256, 300, 384, 512, 1024, 2048, 4096, 16384, 65536} {
		cfg, err := calcAt(limit, 2).Calculate()
		if err != nil {
			// Refusing is a valid outcome for a container too small to host a
			// database at all — but only for genuinely tiny ones.
			if limit >= 128 {
				t.Errorf("limit=%dMB refused: %v", limit, err)
			}
			continue
		}

		pool := mbValue(t, cfg.Settings["innodb_buffer_pool_size"])
		if total := pool + cfg.ReservedMB; total > limit {
			t.Errorf("limit=%dMB: pool %dMB + reserve %dMB = %dMB (%d%% of the limit)",
				limit, pool, cfg.ReservedMB, total, total*100/limit)
		}

		// The chunk must divide the pool, or InnoDB rounds back up on startup
		// and undoes the arithmetic above.
		chunk := mbValue(t, cfg.Settings["innodb_buffer_pool_chunk_size"])
		instances, err := strconv.Atoi(cfg.Settings["innodb_buffer_pool_instances"])
		if err != nil {
			t.Fatalf("limit=%dMB: instances unreadable: %v", limit, err)
		}
		if chunk <= 0 || instances <= 0 {
			t.Errorf("limit=%dMB: chunk=%d instances=%d", limit, chunk, instances)
			continue
		}
		if pool%(chunk*instances) != 0 {
			t.Errorf("limit=%dMB: pool %dMB is not a multiple of chunk %dMB × %d instances; "+
				"InnoDB will round it up", limit, pool, chunk, instances)
		}
	}
}

// TestTinyContainerIsRefusedNotSilentlyOverAllocated: below the point where a
// pool both fits and starts, saying so beats writing a config that OOMs.
func TestTinyContainerIsRefusedNotSilentlyOverAllocated(t *testing.T) {
	// The reserve takes half, so an 8MB container leaves 4MB — below the 8MB
	// InnoDB needs to start at all. There is no size that both fits and works.
	_, err := calcAt(8, 1).Calculate()
	if err == nil {
		t.Error("an 8MB container produced a buffer pool config instead of an error")
	}

	// A small-but-workable container still gets a config, with a warning.
	cfg, err := calcAt(256, 1).Calculate()
	if err != nil {
		t.Fatalf("256MB container refused: %v", err)
	}
	if len(cfg.Warnings) == 0 {
		t.Error("a container this small deserves a warning")
	}
}

// TestProfileWithZeroWorkerMemoryIsRejected: Profiles is an exported mutable map,
// and worker sizing divides by AvgMemoryPerWorker. Inside PID 1 that panic takes
// the container with it.
func TestProfileWithZeroWorkerMemoryIsRejected(t *testing.T) {
	const name Profile = "zz-test-broken"

	Profiles[name] = ProfileConfig{ReservedMemoryMB: 128, MaxMemoryUsage: 0.8}
	defer delete(Profiles, name)

	if _, err := name.GetConfig(); err == nil {
		t.Error("a profile with avg_memory_per_worker=0 was accepted; sizing divides by it")
	}
}

// TestCgroupV2MemoryHasTheSameSanityBoundAsV1: the v1 parser rejects the
// unlimited sentinel, the v2 parser accepted it — turning "no limit" into a
// limit of ~8.8 exabytes that every derived setting then scaled off.
func TestCgroupV2MemoryHasTheSameSanityBoundAsV1(t *testing.T) {
	for _, content := range []string{"9223372036854771712", "-1", "0"} {
		r := &ContainerResources{}
		parseCgroupV2Memory(r, content)
		if r.MemoryLimitMB != 0 {
			t.Errorf("memory.max %q produced a limit of %dMB", content, r.MemoryLimitMB)
		}
	}

	// A real limit still parses.
	r := &ContainerResources{}
	parseCgroupV2Memory(r, "536870912")
	if r.MemoryLimitMB != 512 {
		t.Errorf("memory.max 536870912 gave %dMB, want 512MB", r.MemoryLimitMB)
	}
}

func mbValue(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(strings.TrimSuffix(s, "M"))
	if err != nil {
		t.Fatalf("unreadable size %q: %v", s, err)
	}
	return v
}
