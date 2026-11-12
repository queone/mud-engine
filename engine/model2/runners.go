package model2

import "github.com/queone/mud-engine/engine"

// runner is a player on base and the pitcher charged if the runner scores.
type runner struct {
	id      string
	pitcher string
	speed   int
}

// force advances runners ahead of a walk or hit by pitch and puts the batter on first.
func (g *game) force(br *runner) {
	if g.bases[0] != nil {
		if g.bases[1] != nil {
			if g.bases[2] != nil {
				g.scoreRun(g.bases[2], true)
			}
			g.bases[2] = g.bases[1]
		}
		g.bases[1] = g.bases[0]
	}
	g.bases[0] = br
}

// single scores the runner from third, sends the runner from second home or
// to third, sends the runner from first to third or second, and puts the
// batter on first.
func (g *game) single(br *runner) {
	if r := g.bases[2]; r != nil {
		g.scoreRun(r, true)
		g.bases[2] = nil
	}
	if r := g.bases[1]; r != nil {
		g.bases[1] = nil
		if g.rng.Float64() < advance(advSecondOnSingle, r.speed) {
			g.scoreRun(r, true)
		} else {
			g.bases[2] = r
		}
	}
	if r := g.bases[0]; r != nil {
		g.bases[0] = nil
		if g.bases[2] == nil && g.rng.Float64() < advance(advFirstOnSingle, r.speed) {
			g.bases[2] = r
		} else {
			g.bases[1] = r
		}
	}
	g.bases[0] = br
}

// double scores the runners from third and second, sends the runner from
// first home or to third, and puts the batter on second.
func (g *game) double(br *runner) {
	for _, i := range []int{2, 1} {
		if r := g.bases[i]; r != nil {
			g.scoreRun(r, true)
			g.bases[i] = nil
		}
	}
	if r := g.bases[0]; r != nil {
		g.bases[0] = nil
		if g.rng.Float64() < advance(advFirstOnDouble, r.speed) {
			g.scoreRun(r, true)
		} else {
			g.bases[2] = r
		}
	}
	g.bases[1] = br
}

// triple scores every runner and puts the batter on third.
func (g *game) triple(br *runner) {
	g.clearBases(true)
	g.bases[2] = br
}

// homer scores every runner and the batter.
func (g *game) homer(br *runner) {
	g.clearBases(true)
	g.scoreRun(br, true)
}

// clearBases scores every runner from third down to first.
func (g *game) clearBases(rbi bool) {
	for i := 2; i >= 0; i-- {
		if r := g.bases[i]; r != nil {
			g.scoreRun(r, rbi)
			g.bases[i] = nil
		}
	}
}

// groundOut retires the batter, turns a double play by chance with a runner
// on first and fewer than two outs, and lets runners advance by chance.
func (g *game) groundOut(fld *team) string {
	if g.outs < 2 && g.bases[0] != nil && g.rng.Float64() < doublePlay {
		g.bases[0] = nil
		g.recordOuts(fld, 2)
		if g.outs < 3 {
			if r := g.bases[2]; r != nil {
				g.scoreRun(r, false)
				g.bases[2] = nil
			}
			if r := g.bases[1]; r != nil {
				g.bases[1] = nil
				g.bases[2] = r
			}
		}
		return engine.OutcomeDP
	}
	g.recordOuts(fld, 1)
	if g.outs < 3 {
		if r := g.bases[2]; r != nil && g.rng.Float64() < advance(advThirdOnGround, r.speed) {
			g.scoreRun(r, true)
			g.bases[2] = nil
		}
		if r := g.bases[1]; r != nil && g.bases[2] == nil && g.rng.Float64() < advance(advSecondOnGround, r.speed) {
			g.bases[1] = nil
			g.bases[2] = r
		}
	}
	return engine.OutcomeGO
}

// airOut retires the batter and lets the runner on third score (a sacrifice
// fly) or the runner on second tag to third by chance.
func (g *game) airOut(fld *team) string {
	g.recordOuts(fld, 1)
	outcome := engine.OutcomeAO
	if g.outs < 3 {
		if r := g.bases[2]; r != nil && g.rng.Float64() < advance(advThirdOnAir, r.speed) {
			g.scoreRun(r, true)
			g.bases[2] = nil
			outcome = engine.OutcomeSF
		}
		if r := g.bases[1]; r != nil && g.bases[2] == nil && g.rng.Float64() < advance(advSecondOnAir, r.speed) {
			g.bases[1] = nil
			g.bases[2] = r
		}
	}
	return outcome
}

// steal resolves a possible attempt by the runner on first to take second.
// It reports whether an attempt happened and logs it as its own play.
func (g *game) steal(bat, fld *team, batter engine.Player) bool {
	r := g.bases[0]
	if g.rng.Float64() >= stealTry(r.speed) {
		return false
	}
	before := g.snapshot()
	g.beginPlay(nil)
	outcome := engine.OutcomeSB
	if g.rng.Float64() < stealSuccess(r.speed, fld.catcher) {
		g.bases[1], g.bases[0] = r, nil
		bat.batter(r.id).SB++
	} else {
		g.bases[0] = nil
		bat.batter(r.id).CS++
		g.recordOuts(fld, 1)
		outcome = engine.OutcomeCS
	}
	g.endPlay(before, batter.ID, fld.cur.pitcher.ID, outcome)
	return true
}
