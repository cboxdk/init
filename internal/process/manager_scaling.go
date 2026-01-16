package process

import (
	"context"
	"fmt"

	"github.com/cboxdk/init/internal/metrics"
)

// ScaleProcess changes the number of instances for a process.
// Scaling to zero is treated as a stop operation. Scaling from zero
// is treated as a start operation.
func (m *Manager) ScaleProcess(ctx context.Context, name string, desiredScale int) error {
	// Input validation
	if name == "" {
		return fmt.Errorf("process name cannot be empty")
	}
	if desiredScale > m.maxProcessScale {
		return fmt.Errorf("desired scale %d exceeds maximum (%d)", desiredScale, m.maxProcessScale)
	}

	m.mu.RLock()
	procCfg, ok := m.config.Processes[name]
	sup, supOk := m.processes[name]
	m.mu.RUnlock()

	if !ok || !supOk {
		return fmt.Errorf("process %s not found", name)
	}

	// Check if scale exceeds max_scale limit
	if procCfg.MaxScale > 0 && desiredScale > procCfg.MaxScale {
		return fmt.Errorf("process %s cannot scale above max_scale=%d", name, procCfg.MaxScale)
	}

	// Check if process type supports scaling
	if procCfg.Type == "oneshot" {
		return fmt.Errorf("oneshot processes cannot be scaled (type: oneshot)")
	}

	currentScale := len(sup.GetInstances())

	// Handle scale-to-zero as stop
	if desiredScale <= 0 {
		return m.scaleToZero(ctx, name, sup, currentScale)
	}

	// If currently stopped and desired >=1, treat as start
	if currentScale == 0 {
		return m.scaleFromZero(ctx, name, sup, desiredScale)
	}

	// Idempotent check
	if currentScale == desiredScale {
		m.logger.Info("Process already at desired scale",
			"name", name,
			"scale", currentScale,
		)
		return nil
	}

	m.logger.Info("Scale operation requested",
		"name", name,
		"current", currentScale,
		"desired", desiredScale,
	)

	// SCALE UP: Start new instances
	if desiredScale > currentScale {
		if err := sup.ScaleUp(ctx, desiredScale); err != nil {
			return fmt.Errorf("scale up failed: %w", err)
		}

		m.logger.Info("Scale up completed",
			"name", name,
			"new_scale", desiredScale,
		)
		return m.updateScaleConfig(name, desiredScale)
	}

	// SCALE DOWN: Stop excess instances
	if desiredScale < currentScale {
		if err := sup.ScaleDown(ctx, desiredScale); err != nil {
			return fmt.Errorf("scale down failed: %w", err)
		}

		m.logger.Info("Scale down completed",
			"name", name,
			"new_scale", desiredScale,
		)
		return m.updateScaleConfig(name, desiredScale)
	}

	return nil
}

// scaleToZero handles scaling a process to zero instances (stop).
func (m *Manager) scaleToZero(ctx context.Context, name string, sup *Supervisor, currentScale int) error {
	if currentScale == 0 {
		m.logger.Info("Process already stopped", "name", name)
		return nil
	}
	m.logger.Info("Scale request to zero treated as stop", "name", name)
	stopCtx, cancel := context.WithTimeout(ctx, m.processStopTimeout)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		return fmt.Errorf("failed to stop process %s for scale 0: %w", name, err)
	}
	return m.updateScaleConfig(name, 0)
}

// scaleFromZero handles scaling a process from zero instances (start).
func (m *Manager) scaleFromZero(ctx context.Context, name string, sup *Supervisor, desiredScale int) error {
	if desiredScale == 0 {
		m.logger.Info("Process already stopped", "name", name)
		return nil
	}
	m.logger.Info("Scale up from zero treated as start",
		"name", name,
		"desired", desiredScale,
	)
	if cfg := m.config.Processes[name]; cfg != nil {
		cfg.Scale = desiredScale
	}
	if err := sup.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start process %s for scale %d: %w", name, desiredScale, err)
	}
	metrics.SetDesiredScale(name, desiredScale)
	if desiredScale == 1 {
		return nil
	}
	// If desired > 1, we need to scale up further after initial start
	currentScale := len(sup.GetInstances())
	if currentScale < desiredScale {
		if err := sup.ScaleUp(ctx, desiredScale); err != nil {
			return fmt.Errorf("scale up failed after start: %w", err)
		}
	}
	return nil
}

// updateScaleConfig updates the scale in config and metrics.
func (m *Manager) updateScaleConfig(name string, scale int) error {
	m.mu.Lock()
	if cfg := m.config.Processes[name]; cfg != nil {
		cfg.Scale = scale
	}
	m.mu.Unlock()
	metrics.SetDesiredScale(name, scale)
	return nil
}

// AdjustScale modifies the desired scale relative to the current configuration.
// Positive delta scales up, negative delta scales down. Scaling to zero is
// treated as a stop, and zero->positive is treated as a start.
func (m *Manager) AdjustScale(ctx context.Context, name string, delta int) error {
	if delta == 0 {
		return nil
	}

	m.mu.RLock()
	cfg, ok := m.config.Processes[name]
	sup, supOk := m.processes[name]
	m.mu.RUnlock()
	if !ok || !supOk {
		return fmt.Errorf("process %s not found", name)
	}

	currentScale := len(sup.GetInstances())

	target := currentScale + delta
	if target < 0 {
		target = 0
	}

	// Enforce max_scale limit
	if cfg.MaxScale > 0 && target > cfg.MaxScale {
		return fmt.Errorf("process %s cannot scale above max_scale=%d", name, cfg.MaxScale)
	}

	if target == currentScale {
		m.logger.Info("Process already at desired scale",
			"name", name,
			"scale", cfg.Scale,
		)
		return nil
	}

	return m.ScaleProcess(ctx, name, target)
}
