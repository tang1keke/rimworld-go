package animalview

import "rimworld-go/internal/domain/pawn/body"

// AsthmaOf renders affected lungs in part order from the shared medical body.
func AsthmaOf(b *body.Body) []Asthma {
	if b == nil {
		return nil
	}
	var out []Asthma
	for _, c := range b.Conds {
		if c.Kind == body.CondAsthma {
			out = append(out, Asthma{Lung: b.PartLabel(c.Part), SeverityPct: int(c.Sev) / 10})
		}
	}
	return out
}
