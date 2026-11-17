// Package modloader owns declarative mod validation and the runtime boundary
// consumed by the game application.
package modloader

import (
	"errors"
	"fmt"
	"slices"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/domain/economy/item"
)

// VersionPolicy supplies semantic-version operations without making the
// deterministic application depend on a third-party implementation.
type VersionPolicy interface {
	Valid(version string) bool
	Compare(left, right string) int
}

// Runtime is the small mod-content contract the simulation needs. Registry
// keeps declaration, asset and provenance operations behind this boundary.
type Runtime interface {
	Content() savefmt.Content
	ItemValue(kind item.Kind) int
}

// ErrContentMismatch means the save's active mod set or definitions differ.
var ErrContentMismatch = errors.New("save mod content mismatch")

// ContentOf returns the active mod stamp, or the empty stamp for core-only
// games whose legacy save headers intentionally remain unchanged.
func ContentOf(runtime Runtime) savefmt.Content {
	if runtime == nil {
		return savefmt.Content{}
	}
	return runtime.Content()
}

// ItemValue resolves a core item through the active registry when one is
// present; without mods the native definition remains authoritative.
func ItemValue(runtime Runtime, kind item.Kind) int {
	if runtime == nil {
		return kind.Value()
	}
	return runtime.ItemValue(kind)
}

// CheckContent rejects modded saves unless the exact active content is
// available. Legacy core-only headers remain readable by the core runtime.
func CheckContent(saved savefmt.Content, runtime Runtime) error {
	if runtime == nil {
		if len(saved.Plugins) == 0 || len(saved.Plugins) == 1 && saved.Plugins[0] == "core" {
			return nil
		}
		return fmt.Errorf("%w: save requires %v, runtime has core only", ErrContentMismatch, saved.Plugins)
	}
	active := runtime.Content()
	if !slices.Equal(saved.Plugins, active.Plugins) || saved.Hash != active.Hash {
		return fmt.Errorf("%w: save %v (%016x), runtime %v (%016x)",
			ErrContentMismatch, saved.Plugins, saved.Hash, active.Plugins, active.Hash)
	}
	return nil
}
