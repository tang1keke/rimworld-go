package item

import "strings"

// Definition is the core item's data-definition view. It is built from the
// actual tables used by Kind.String and Kind.Value, not a parallel catalogue.
type Definition struct {
	ID    string
	Label string
	Value int
	Kind  Kind
}

// Definitions lists every core item in its stable saved Kind order.
// Mod content uses this same definition shape and ID namespace.
func Definitions() []Definition {
	out := make([]Definition, 0, numKinds)
	for k := Kind(0); k < numKinds; k++ {
		label := k.String()
		id := strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(label))
		out = append(out, Definition{
			ID: "core:" + id, Label: label, Value: k.Value(), Kind: k,
		})
	}
	return out
}
