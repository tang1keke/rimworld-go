package mood

import "rimworld-go/internal/domain/pawn/expectations"

// ExpectationsForWealth selects a state thought from the shared wealth bands.
func ExpectationsForWealth(wealth int) Kind {
	return ExpectationsExtremelyLow + Kind(expectations.ForWealth(wealth))
}
