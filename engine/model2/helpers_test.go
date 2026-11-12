package model2

import (
	"fmt"
	"github.com/queone/mud-engine/engine"
	"testing"
)

var (
	avgBat = engine.Batting{Contact: 50, Power: 50, Eye: 50, Speed: 50, Defense: 50}
	avgPit = engine.Pitching{Stuff: 50, Control: 50, Movement: 50, Stamina: 50}

	lineupPositions = [9]string{"C", "1B", "2B", "3B", "SS", "LF", "CF", "RF", "DH"}
	lineupHands     = [9]engine.Hand{engine.Right, engine.Left, engine.Right, engine.Right, engine.Left, engine.Right, engine.Switch, engine.Left, engine.Right}
	bullpenHands    = []engine.Hand{engine.Right, engine.Left, engine.Right, engine.Right, engine.Left}
)

func batter(id string, bats engine.Hand, b engine.Batting) engine.Player {
	return engine.Player{ID: id, Name: id, Bats: bats, Throws: engine.Right, Batting: &b}
}

func pitcher(id string, throws engine.Hand, p engine.Pitching) engine.Player {
	return engine.Player{ID: id, Name: id, Bats: engine.Right, Throws: throws, Pitching: &p}
}

// handedClub builds a club with the given ratings everywhere and the given
// hands: nil hands use the mixed defaults.
func handedClub(name, prefix string, b engine.Batting, p engine.Pitching, bats []engine.Hand, throws []engine.Hand) engine.Club {
	c := engine.Club{Name: name}
	for i := range c.Lineup {
		h := lineupHands[i]
		if bats != nil {
			h = bats[i%len(bats)]
		}
		c.Lineup[i] = engine.Slot{Player: batter(fmt.Sprintf("%sb%d", prefix, i+1), h, b), Position: lineupPositions[i]}
	}
	th := func(i int) engine.Hand {
		if throws != nil {
			return throws[i%len(throws)]
		}
		return bullpenHands[i%len(bullpenHands)]
	}
	c.Starter = pitcher(prefix+"sp", th(0), p)
	c.Bullpen = engine.Bullpen{
		Closer: pitcher(prefix+"cl", th(1), p),
		Setup:  pitcher(prefix+"su", th(2), p),
		Others: []engine.Player{pitcher(prefix+"r1", th(3), p), pitcher(prefix+"r2", th(4), p), pitcher(prefix+"r3", th(0), p)},
	}
	return c
}

// ratedClub builds a club whose every batter and pitcher carries the given ratings.
func ratedClub(name, prefix string, b engine.Batting, p engine.Pitching) engine.Club {
	return handedClub(name, prefix, b, p, nil, nil)
}

func averageClub(name, prefix string) engine.Club { return ratedClub(name, prefix, avgBat, avgPit) }

func mustSimulate(t *testing.T, home, away engine.Club, seed engine.Seed) engine.Result {
	t.Helper()
	r, err := Simulate(home, away, seed)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// series simulates n games with seeds derived from world, season 1, game i.
func series(t *testing.T, home, away engine.Club, world uint64, n int) []engine.Result {
	t.Helper()
	out := make([]engine.Result, 0, n)
	for i := range n {
		out = append(out, mustSimulate(t, home, away, engine.DeriveSeed(world, 1, uint64(i))))
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
