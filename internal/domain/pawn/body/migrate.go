package body

// MigrateReplacements upgrades nested pre-v18 animal body saves. Colonist
// bodies migrate through the savedBody adapter instead. The old field is
// read-only and cleared on first import.
func MigrateReplacements(b *Body) {
	if b == nil || len(b.Fitted) == 0 {
		return
	}
	if b.Replacements == nil {
		b.Replacements = ReplacementsFrom(b.Fitted)
	}
	b.Fitted = nil
}
