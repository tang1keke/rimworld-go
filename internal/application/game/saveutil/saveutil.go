// Package saveutil contains pure legacy-save conversion helpers. Keeping
// them outside game pays down its SOLID source-line budget as fields grow.
package saveutil

import (
	"encoding/binary"
	"hash/fnv"
	mathbits "math/bits"
	"rimworld-go/internal/domain/world/terrain"
)

// bitIndexes lists the set bits of a bitset in ascending order.
func BitIndexes(bits []uint64) []int {
	n := 0
	for _, w := range bits {
		n += mathbits.OnesCount64(w)
	}
	out := make([]int, 0, n)
	for wi, w := range bits {
		for w != 0 {
			out = append(out, wi*64+mathbits.TrailingZeros64(w))
			w &= w - 1
		}
	}
	return out
}

// setMap installs a decoded map, keeping the empty one gob leaves nil.
func SetMap[K comparable, V any](dst *map[K]V, src map[K]V) {
	if src != nil {
		*dst = src
	}
}

// terrainHash is an FNV-64a hash of the terrain: the geology seed of a
// save made before geology existed.
func TerrainHash(t terrain.Map) uint64 {
	h := fnv.New64a()
	var hdr [16]byte
	binary.LittleEndian.PutUint64(hdr[:8], uint64(t.W))
	binary.LittleEndian.PutUint64(hdr[8:], uint64(t.H))
	_, _ = h.Write(hdr[:])
	cells := make([]byte, len(t.Cells))
	for i, c := range t.Cells {
		cells[i] = byte(c)
	}
	_, _ = h.Write(cells)
	return h.Sum64()
}
