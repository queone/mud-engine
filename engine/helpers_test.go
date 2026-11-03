package engine

import (
	"fmt"
	"testing"
)

var (
	avgBat = Batting{Contact: 50, Power: 50, Eye: 50, Speed: 50, Defense: 50}
	avgPit = Pitching{Stuff: 50, Control: 50, Movement: 50, Stamina: 50}

	lineupPositions = [9]string{"C", "1B", "2B", "3B", "SS", "LF", "CF", "RF", "DH"}
	lineupHands     = [9]Hand{Right, Left, Right, Right, Left, Right, Switch, Left, Right}
	bullpenHands    = []Hand{Right, Left, Right, Right, Left}
)

func batter(id string, bats Hand, b Batting) Player {
	return Player{ID: id, Name: id, Bats: bats, Throws: Right, Batting: &b}
}

func pitcher(id string, throws Hand, p Pitching) Player {
	return Player{ID: id, Name: id, Bats: Right, Throws: throws, Pitching: &p}
}

// handedClub builds a club with the given ratings everywhere and the given
// hands: nil hands use the mixed defaults.
func handedClub(name, prefix string, b Batting, p Pitching, bats []Hand, throws []Hand) Club {
	c := Club{Name: name}
	for i := range c.Lineup {
		h := lineupHands[i]
		if bats != nil {
			h = bats[i%len(bats)]
		}
		c.Lineup[i] = Slot{Player: batter(fmt.Sprintf("%sb%d", prefix, i+1), h, b), Position: lineupPositions[i]}
	}
	th := func(i int) Hand {
		if throws != nil {
			return throws[i%len(throws)]
		}
		return bullpenHands[i%len(bullpenHands)]
	}
	c.Starter = pitcher(prefix+"sp", th(0), p)
	c.Bullpen = Bullpen{
		Closer: pitcher(prefix+"cl", th(1), p),
		Setup:  pitcher(prefix+"su", th(2), p),
		Others: []Player{pitcher(prefix+"r1", th(3), p), pitcher(prefix+"r2", th(4), p), pitcher(prefix+"r3", th(0), p)},
	}
	return c
}

// ratedClub builds a club whose every batter and pitcher carries the given ratings.
func ratedClub(name, prefix string, b Batting, p Pitching) Club {
	return handedClub(name, prefix, b, p, nil, nil)
}

func averageClub(name, prefix string) Club { return ratedClub(name, prefix, avgBat, avgPit) }

func mustSimulate(t *testing.T, home, away Club, seed Seed) Result {
	t.Helper()
	r, err := Simulate(home, away, seed)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// series simulates n games with seeds derived from world, season 1, game i.
func series(t *testing.T, home, away Club, world uint64, n int) []Result {
	t.Helper()
	out := make([]Result, 0, n)
	for i := range n {
		out = append(out, mustSimulate(t, home, away, DeriveSeed(world, 1, uint64(i))))
	}
	return out
}

func runnersOn(bases [3]string) int {
	n := 0
	for _, b := range bases {
		if b != "" {
			n++
		}
	}
	return n
}
