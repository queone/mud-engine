package engine

// fieldOrder is the positions and outfield gaps a ball in play goes toward,
// in the order a direction draw walks them: LC and RC are the left-center
// and right-center gaps, which only hits and homers reach.
var fieldOrder = [11]string{"P", "C", "1B", "2B", "SS", "3B", "LF", "LC", "CF", "RC", "RF"}

// directionRows are each ball-in-play outcome's shares in fieldOrder's
// order for a right-handed batter: he pulls toward left field and the left
// side of the infield. A left-handed batter's row mirrors it. Hits and
// homers spread over five outfield zones; an out goes to the fielder who
// made it (Director review 2026-11-12).
var directionRows = map[string][11]float64{
	//                 P    C    1B   2B   SS   3B   LF   LC   CF   RC   RF
	Outcome1B: {0, 0, 0, 0, 0, 0, .32, .10, .26, .09, .23},
	Outcome2B: {0, 0, 0, 0, 0, 0, .30, .20, .10, .15, .25},
	Outcome3B: {0, 0, 0, 0, 0, 0, .10, .15, .15, .30, .30},
	OutcomeHR: {0, 0, 0, 0, 0, 0, .38, .22, .18, .12, .10},
	OutcomeGO: {.05, 0, .15, .26, .31, .23, 0, 0, 0, 0, 0},
	OutcomeDP: {.04, 0, .08, .28, .38, .22, 0, 0, 0, 0, 0},
	OutcomeAO: {0, .01, .02, .05, .06, .06, .27, 0, .31, 0, .22},
	OutcomeSF: {0, 0, 0, 0, 0, 0, .35, 0, .40, 0, .25},
}

// mirrorOf is each position's index across the field: LF with RF, LC with
// RC, 3B with 1B, SS with 2B; the pitcher, the catcher, and center field
// stay.
var mirrorOf = [11]int{0, 1, 5, 4, 3, 2, 10, 9, 8, 7, 6}

// battingLeft says whether a batter hits from the left side against the
// pitcher: a switch hitter bats from the side opposite the pitcher's hand.
func battingLeft(bats, throws Hand) bool {
	if bats == Switch {
		return throws != Left
	}
	return bats == Left
}

// directionShares is the outcome's row for the batter's side; ok is false for
// an outcome that puts no ball in play.
func directionShares(outcome string, left bool) ([11]float64, bool) {
	row, ok := directionRows[outcome]
	if !ok || !left {
		return row, ok
	}
	var out [11]float64
	for i := range row {
		out[i] = row[mirrorOf[i]]
	}
	return out, true
}

// toward walks the outcome's shares for the batter's side in fieldOrder'
// order with the draw u and answers the position it lands on; a u past the
// shares' rounded sum lands on the last position the row allows.
func toward(outcome string, left bool, u float64) string {
	shares, ok := directionShares(outcome, left)
	if !ok {
		return ""
	}
	last, sum := "", 0.0
	for i, s := range shares {
		if s == 0 {
			continue
		}
		last = fieldOrder[i]
		sum += s
		if u < sum {
			return last
		}
	}
	return last
}
