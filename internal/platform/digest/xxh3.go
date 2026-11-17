package digest

import "github.com/zeebo/xxh3"

// XXH3 returns the non-cryptographic XXH3-64 digest of data.
func XXH3(data []byte) uint64 { return xxh3.Hash(data) }
