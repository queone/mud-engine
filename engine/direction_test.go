package engine

import (
	"math"
	"testing"
)

// A batter hits from the left side when he bats left, or bats both ways
// against a pitcher who does not throw left.
func TestTheBattingSideFollowsTheHands(t *testing.T) {
	for _, c := range []struct {
		bats, throws Hand
		left         bool
	}{
		{Left, Right, true}, {Left, Left, true},
		{Right, Right, false}, {Right, Left, false},
		{Switch, Right, true}, {Switch, Left, false},
	} {
		if got := battingLeft(c.bats, c.throws); got != c.left {
			t.Errorf("bats %s against %s: left %v, want %v", c.bats, c.throws, got, c.left)
		}
	}
}

// Each row sums to 1, and a left-handed batter's row mirrors a right-handed
// batter's across the field.
func TestTheDirectionRowsSumToOneAndMirror(t *testing.T) {
	index := map[string]int{}
	for i, p := range fieldOrder {
		index[p] = i
	}
	mirror := map[string]string{"P": "P", "C": "C", "1B": "3B", "2B": "SS", "SS": "2B", "3B": "1B", "LF": "RF", "LC": "RC", "CF": "CF", "RC": "LC", "RF": "LF"}
	for outcome := range directionRows {
		right, _ := directionShares(outcome, false)
		left, _ := directionShares(outcome, true)
		sum := 0.0
		for i, s := range right {
			sum += s
			if left[index[mirror[fieldOrder[i]]]] != s {
				t.Errorf("%s: %s's share %v is not mirrored", outcome, fieldOrder[i], s)
			}
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Errorf("%s: the shares sum to %v", outcome, sum)
		}
	}
	if _, ok := directionShares(OutcomeK, false); ok {
		t.Error("a strikeout has a direction row")
	}
}

// directionCell counts one outcome's balls in play for one batting side, by
// position.
type directionCell struct {
	n       int
	byPlace map[string]int
}

// Every ball in play has a direction its row allows for the batter's side,
// every other play has none, and the shares land within 4 standard errors of
// the rows, sqrt(p*(1-p)/N) with N the outcome's count for that side, across
// 1,000 seeded games of mixed hands, switch hitters on the side opposite the
// pitcher's hand.
func TestDirectionsFollowTheRowsAndTheBattingSide(t *testing.T) {
	home := averageClub("Home", "h")
	away := handedClub("Away", "a", avgBat, avgPit, []Hand{Left, Switch, Right}, []Hand{Left, Right})
	hands := map[string]Player{}
	for _, c := range []Club{home, away} {
		for _, p := range c.players() {
			hands[p.ID] = p
		}
	}
	cells := map[string]map[bool]*directionCell{}
	for outcome := range directionRows {
		cells[outcome] = map[bool]*directionCell{false: {byPlace: map[string]int{}}, true: {byPlace: map[string]int{}}}
	}
	for _, r := range series(t, home, away, 4242, 1000) {
		for _, p := range r.Plays {
			cell, inPlay := cells[p.Outcome]
			if !inPlay {
				if p.Toward != "" {
					t.Fatalf("a %q play has direction %s", p.Outcome, p.Toward)
				}
				continue
			}
			batter, pitcher := hands[p.Batter], hands[p.Pitcher]
			left := batter.Bats == Left || (batter.Bats == Switch && pitcher.Throws == Right)
			shares, _ := directionShares(p.Outcome, left)
			allowed := false
			for i, s := range shares {
				allowed = allowed || (s > 0 && fieldOrder[i] == p.Toward)
			}
			if !allowed {
				t.Fatalf("a %s by a batter hitting from the left %v went toward %q, which its row forbids", p.Outcome, left, p.Toward)
			}
			cell[left].n++
			cell[left].byPlace[p.Toward]++
		}
	}
	for outcome, sides := range cells {
		for left, cell := range sides {
			if cell.n == 0 {
				t.Errorf("no %s by a batter hitting from the left %v", outcome, left)
				continue
			}
			shares, _ := directionShares(outcome, left)
			for i, p := range shares {
				if p == 0 {
					continue
				}
				got := float64(cell.byPlace[fieldOrder[i]]) / float64(cell.n)
				se := math.Sqrt(p * (1 - p) / float64(cell.n))
				if math.Abs(got-p) > 4*se {
					t.Errorf("%s, left %v: %s share %.3f of %d, want %.3f within %.3f", outcome, left, fieldOrder[i], got, cell.n, p, 4*se)
				}
			}
		}
	}
}
