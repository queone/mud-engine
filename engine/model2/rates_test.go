package model2

import (
	"github.com/queone/mud-engine/engine"
	"math"
	"testing"
)

func TestAverageMatchupSitsNearTheAnchors(t *testing.T) {
	pp := matchup(&avgBat, engine.Left, &avgPit, engine.Right, 50)
	checks := []struct {
		name      string
		got, want float64
	}{
		{"k", pp.k, anchorK}, {"bb", pp.bb, anchorBB}, {"hbp", pp.hbp, anchorHBP},
		{"hr", pp.hr, anchorHR}, {"hit", pp.hit, anchorBABIP}, {"double", pp.double, anchor2B}, {"triple", pp.triple, anchor3B},
	}
	for _, c := range checks {
		if math.Abs(c.got-c.want) > 0.03 {
			t.Errorf("%s = %.4f, want near %.4f", c.name, c.got, c.want)
		}
	}
	if sum := pp.k + pp.bb + pp.hbp; sum >= 1 {
		t.Errorf("three true outcomes sum to %.3f", sum)
	}
}

func TestRatingsMoveRatesInTheRightDirection(t *testing.T) {
	with := func(b engine.Batting, p engine.Pitching) plateProbs {
		return matchup(&b, engine.Left, &p, engine.Right, 50)
	}
	base := with(avgBat, avgPit)
	if with(engine.Batting{Contact: 75, Power: 50, Eye: 50, Speed: 50, Defense: 50}, avgPit).k >= base.k {
		t.Error("more contact did not cut strikeouts")
	}
	if with(avgBat, engine.Pitching{Stuff: 75, Control: 50, Movement: 50, Stamina: 50}).k <= base.k {
		t.Error("more stuff did not add strikeouts")
	}
	if with(engine.Batting{Contact: 50, Power: 50, Eye: 75, Speed: 50, Defense: 50}, avgPit).bb <= base.bb {
		t.Error("more eye did not add walks")
	}
	if with(avgBat, engine.Pitching{Stuff: 50, Control: 75, Movement: 50, Stamina: 50}).bb >= base.bb || with(avgBat, engine.Pitching{Stuff: 50, Control: 75, Movement: 50, Stamina: 50}).hbp >= base.hbp {
		t.Error("more control did not cut walks and hit batters")
	}
	if with(engine.Batting{Contact: 50, Power: 75, Eye: 50, Speed: 50, Defense: 50}, avgPit).hr <= base.hr {
		t.Error("more power did not add home runs")
	}
	if with(avgBat, engine.Pitching{Stuff: 50, Control: 50, Movement: 75, Stamina: 50}).hr >= base.hr || with(avgBat, engine.Pitching{Stuff: 50, Control: 50, Movement: 75, Stamina: 50}).hit >= base.hit {
		t.Error("more movement did not cut home runs and hits")
	}
	if with(engine.Batting{Contact: 50, Power: 50, Eye: 50, Speed: 75, Defense: 50}, avgPit).triple <= base.triple {
		t.Error("more speed did not add triples")
	}
	if matchup(&avgBat, engine.Left, &avgPit, engine.Right, 75).hit >= base.hit {
		t.Error("better defense did not cut hits")
	}
	same := matchup(&avgBat, engine.Right, &avgPit, engine.Right, 50)
	if same.k <= base.k || same.bb >= base.bb || same.hr >= base.hr || same.hit >= base.hit {
		t.Error("a same-handed matchup did not favor the pitcher")
	}
	sw := matchup(&avgBat, engine.Switch, &avgPit, engine.Right, 50)
	if sw.k != base.k {
		t.Error("a switch hitter did not get the platoon edge against a right-hander")
	}
}

func TestExtremeRatingsStayInsideTheCaps(t *testing.T) {
	pp := matchup(&engine.Batting{Contact: 1, Power: 99, Eye: 99, Speed: 99, Defense: 1}, engine.Left, &engine.Pitching{Stuff: 99, Control: 1, Movement: 1, Stamina: 1}, engine.Right, 1)
	if sum := pp.k + pp.bb + pp.hbp; sum > maxThreeTrue+1e-9 {
		t.Errorf("three true outcomes sum to %.3f, cap is %.2f", sum, maxThreeTrue)
	}
	if sum := pp.double + pp.triple; sum > maxExtraBase+1e-9 {
		t.Errorf("extra-base share %.3f, cap is %.2f", sum, maxExtraBase)
	}
	for _, v := range []float64{pp.k, pp.bb, pp.hbp, pp.hr, pp.hit, pp.double, pp.triple} {
		if v <= 0 || v >= 1 {
			t.Errorf("probability %.4f is outside (0,1)", v)
		}
	}
}

func TestStealAndAdvanceChancesFollowSpeedAndCatcher(t *testing.T) {
	if stealTry(75) <= stealTry(50) || stealTry(25) >= stealTry(50) {
		t.Error("steal attempts do not follow speed")
	}
	if stealSuccess(75, 50) <= stealSuccess(50, 50) || stealSuccess(50, 75) >= stealSuccess(50, 50) {
		t.Error("steal success does not follow speed and the catcher")
	}
	if advance(advFirstOnDouble, 75) <= advFirstOnDouble || advance(advFirstOnDouble, 25) >= advFirstOnDouble {
		t.Error("extra bases do not follow speed")
	}
	if got := advance(advFirstOnDouble, 50); math.Abs(got-advFirstOnDouble) > 1e-12 {
		t.Errorf("average speed moved the base chance to %.4f", got)
	}
}
