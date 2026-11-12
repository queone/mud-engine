package model2

import (
	"github.com/queone/mud-engine/engine"
	"testing"
)

// bench builds a game in the top of the first with the given runners and outs.
func bench(seed uint64, bases [3]bool, outs int) (*game, *team, *team) {
	away, home := averageClub("Away", "a"), averageClub("Home", "h")
	g := newGame(&away, &home, engine.RawSeed(seed, 1))
	g.inning, g.top, g.outs = 1, true, outs
	fld := g.clubs[1]
	for i, on := range bases {
		if on {
			g.bases[i] = &runner{id: "r" + string(rune('1'+i)), pitcher: fld.cur.pitcher.ID, speed: 50}
			g.clubs[0].batter(g.bases[i].id)
		}
	}
	g.beginPlay(g.clubs[0].batter("bat"))
	return g, g.clubs[0], fld
}

func newBatter() *runner { return &runner{id: "bat", pitcher: "hsp", speed: 50} }

func TestWalkForcesOnlyTheRunnersAhead(t *testing.T) {
	g, _, _ := bench(1, [3]bool{true, true, true}, 0)
	g.force(newBatter())
	if g.curRuns != 1 || g.bases[0].id != "bat" || g.bases[1].id != "r1" || g.bases[2].id != "r2" {
		t.Errorf("bases-loaded walk: runs %d, bases %v", g.curRuns, g.snapshot().bases)
	}
	g, _, _ = bench(1, [3]bool{false, true, false}, 0)
	g.force(newBatter())
	if g.curRuns != 0 || g.bases[1].id != "r2" || g.bases[0].id != "bat" {
		t.Errorf("walk with second only: runs %d, bases %v", g.curRuns, g.snapshot().bases)
	}
}

func TestHitsScoreTheRunnersTheyMust(t *testing.T) {
	g, _, _ := bench(2, [3]bool{false, false, true}, 0)
	g.single(newBatter())
	if g.curRuns != 1 || g.bases[0].id != "bat" {
		t.Errorf("single with third: runs %d", g.curRuns)
	}
	g, _, _ = bench(2, [3]bool{false, true, true}, 0)
	g.double(newBatter())
	if g.curRuns != 2 || g.bases[1].id != "bat" || g.bases[2] != nil {
		t.Errorf("double with second and third: runs %d, bases %v", g.curRuns, g.snapshot().bases)
	}
	g, _, _ = bench(2, [3]bool{true, true, true}, 0)
	g.triple(newBatter())
	if g.curRuns != 3 || g.bases[2].id != "bat" || g.bases[0] != nil || g.bases[1] != nil {
		t.Errorf("triple: runs %d, bases %v", g.curRuns, g.snapshot().bases)
	}
	g, _, _ = bench(2, [3]bool{true, true, true}, 0)
	g.homer(newBatter())
	if g.curRuns != 4 || g.runnersOn() != 0 || g.curRBI.RBI != 4 {
		t.Errorf("grand slam: runs %d, rbi %d, on %d", g.curRuns, g.curRBI.RBI, g.runnersOn())
	}
}

func TestExtraBasesAndOutsVaryBySeed(t *testing.T) {
	scored, held := 0, 0
	for seed := range uint64(200) {
		g, _, _ := bench(seed, [3]bool{false, true, false}, 0)
		g.single(newBatter())
		if g.curRuns == 1 {
			scored++
		} else if g.bases[2] != nil && g.bases[2].id == "r2" {
			held++
		} else {
			t.Fatalf("seed %d: runner from second vanished", seed)
		}
	}
	if scored == 0 || held == 0 {
		t.Errorf("runner on second: scored %d, held %d; want both", scored, held)
	}
	dp, go1 := 0, 0
	for seed := range uint64(200) {
		g, _, fld := bench(seed, [3]bool{true, false, false}, 0)
		switch g.groundOut(fld) {
		case engine.OutcomeDP:
			dp++
			if g.outs != 2 || g.bases[0] != nil {
				t.Fatalf("seed %d: double play left outs %d, first %v", seed, g.outs, g.bases[0])
			}
		case engine.OutcomeGO:
			go1++
			if g.outs != 1 || g.bases[0] == nil {
				t.Fatalf("seed %d: ground out left outs %d", seed, g.outs)
			}
		}
	}
	if dp == 0 || go1 == 0 {
		t.Errorf("ground outs: dp %d, plain %d; want both", dp, go1)
	}
	sf, ao := 0, 0
	for seed := range uint64(200) {
		g, _, fld := bench(seed, [3]bool{false, false, true}, 1)
		switch g.airOut(fld) {
		case engine.OutcomeSF:
			sf++
			if g.curRuns != 1 || g.outs != 2 {
				t.Fatalf("seed %d: sacrifice fly runs %d outs %d", seed, g.curRuns, g.outs)
			}
		case engine.OutcomeAO:
			ao++
		}
	}
	if sf == 0 || ao == 0 {
		t.Errorf("air outs: sf %d, plain %d; want both", sf, ao)
	}
	g, _, fld := bench(3, [3]bool{false, false, true}, 2)
	if got := g.airOut(fld); got != engine.OutcomeAO || g.curRuns != 0 || g.outs != 3 {
		t.Errorf("air out with two outs: %s, runs %d, outs %d", got, g.curRuns, g.outs)
	}
	g, _, fld = bench(3, [3]bool{true, false, false}, 2)
	if got := g.groundOut(fld); got != engine.OutcomeGO || g.outs != 3 {
		t.Errorf("ground out with two outs: %s, outs %d", got, g.outs)
	}
}

func TestStealAttemptsSucceedAndFail(t *testing.T) {
	sb, cs, none := 0, 0, 0
	for seed := range uint64(400) {
		g, bat, fld := bench(seed, [3]bool{true, false, false}, 0)
		g.bases[0].speed = 90
		if !g.steal(bat, fld, bat.club.Lineup[0].Player) {
			none++
			continue
		}
		p := g.plays[len(g.plays)-1]
		switch p.Outcome {
		case engine.OutcomeSB:
			sb++
			if g.bases[1] == nil || g.bases[0] != nil || bat.batter("r1").SB != 1 {
				t.Fatalf("seed %d: stolen base left bases %v", seed, g.snapshot().bases)
			}
		case engine.OutcomeCS:
			cs++
			if g.outs != 1 || g.bases[0] != nil || bat.batter("r1").CS != 1 || fld.pitcher("hsp").Outs != 1 {
				t.Fatalf("seed %d: caught stealing left outs %d", seed, g.outs)
			}
		}
	}
	if sb == 0 || cs == 0 || none == 0 {
		t.Errorf("steals: sb %d, cs %d, none %d; want all three", sb, cs, none)
	}
}
