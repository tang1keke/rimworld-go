// Package behaviorhost adapts opt-in trusted Go behavior providers to the
// public game read-model and typed-command boundary. It never loads plugins.
package behaviorhost

import (
	"fmt"
	"io"
	"rimworld-go/internal/application/game"
	"rimworld-go/internal/application/game/behavior"
)

// Runner is the host for explicitly trusted providers; the game owns the
// authoritative tick and invokes it synchronously after each complete pass.
type Runner struct {
	g        *game.Game
	registry *behavior.Registry
}

// Attach registers trusted Go code against existing saved provider state.
// Call before stepping, not during a running simulation.
func Attach(g *game.Game, providers ...behavior.Provider) (*Runner, error) {
	if g == nil {
		return nil, fmt.Errorf("behavior: nil game")
	}
	reg := behavior.NewRegistry()
	if err := reg.Restore(game.BehaviorStateSnapshot(g)); err != nil {
		return nil, err
	}
	for _, p := range providers {
		if err := reg.Register(p); err != nil {
			return nil, fmt.Errorf("register behavior: %w", err)
		}
	}
	h := &Runner{g: g, registry: reg}
	game.AttachBehaviorRuntime(g, h)
	return h, nil
}

// LoadWithProviders keeps the ordinary save format; unknown provider state is
// preserved and migrations are validated before any provider is attached.
func LoadWithProviders(r io.Reader, providers ...behavior.Provider) (*game.Game, *Runner, game.SaveInfo, error) {
	g, info, err := game.Load(r)
	if err != nil {
		return nil, nil, info, err
	}
	h, err := Attach(g, providers...)
	if err != nil {
		return nil, nil, info, err
	}
	return g, h, info, nil
}

// Step implements game.BehaviorRuntime. No independent clock or goroutines.
func (h *Runner) Step(tick uint64) { h.registry.Step(tick, h.world, h.submit) }

// Export clones provider state into the save without sharing memory.
func (h *Runner) Export() []behavior.StoredState { return h.registry.Export() }

// Failures returns the last due-tick provider errors, keyed by ID and tick.
func (h *Runner) Failures() []behavior.Failure { return h.registry.Failures() }

// Missing lists saved states whose provider code was not registered.
func (h *Runner) Missing() []string { return h.registry.Missing() }
