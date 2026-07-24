// Disable Go's default Multipath TCP for listeners. cbox-init is PID 1 in the
// container, so an MPTCP-backed socket it opens (metrics/health) cannot be
// checkpointed by CRIU — which silently breaks scale-to-zero suspend/resume for
// the whole container. Plain TCP costs nothing here and keeps the process
// checkpointable. Baked into the binary so no runtime env is required.
//
//go:debug multipathtcp=0

package main

func main() {
	Execute()
}
