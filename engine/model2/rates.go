package model2

import (
	"github.com/queone/mud-engine/engine"
	"math"
)

// League anchors: 2010 through 2024 MLB averages, tuned so the calibration
// test lands inside its bands. Rates are per plate appearance unless noted.
const (
	anchorK     = 0.215 // strikeouts
	anchorBB    = 0.083 // walks
	anchorHBP   = 0.011 // hit by pitch
	anchorHR    = 0.045 // home runs per ball in play
	anchorBABIP = 0.295 // hits per ball in play that is not a home run
	anchor2B    = 0.20  // doubles per hit that is not a home run
	anchor3B    = 0.02  // triples per hit that is not a home run
	anchorGB    = 0.44  // ground balls per out in play
	anchorSBTry = 0.065 // steal attempts per chance (runner on first, second open)
	anchorSB    = 0.75  // steal success
	platoon     = 1.10  // odds shift for a same-handed matchup
)

// Rating spreads: the odds multiplier that 25 rating points apply.
const (
	spreadK        = 1.5
	spreadBB       = 1.5
	spreadHBP      = 1.3
	spreadHR       = 1.6
	spreadHRPitch  = 1.6
	spreadBABIPCon = 1.10
	spreadBABIPSpd = 1.10
	spreadBABIPPit = 1.08
	spreadBABIPDef = 1.12
	spread2B       = 1.3
	spread3B       = 2.0
	spreadSBTry    = 4.0
	spreadSBRun    = 1.6
	spreadSBCatch  = 1.4
	spreadAdvance  = 1.5
)

// Base chances that a runner takes the extra base, and the in-play rules.
const (
	advSecondOnSingle = 0.64 // runner on second scores on a single
	advFirstOnSingle  = 0.28 // runner on first reaches third on a single
	advFirstOnDouble  = 0.47 // runner on first scores on a double
	advThirdOnGround  = 0.55 // runner on third scores on a ground out
	advSecondOnGround = 0.50 // runner on second reaches third on a ground out
	advThirdOnAir     = 0.60 // runner on third scores on an air out
	advSecondOnAir    = 0.25 // runner on second reaches third on an air out
	doublePlay        = 0.42 // ground out with a runner on first becomes two outs
	maxThreeTrue      = 0.90 // cap on strikeout plus walk plus hit by pitch
	maxExtraBase      = 0.90 // cap on double plus triple share
)

// mult returns the odds multiplier for a rating: spread per 25 points from 50.
func mult(rating int, spread float64) float64 {
	return math.Pow(spread, float64(rating-50)/25)
}

func odds(p float64) float64 { return p / (1 - p) }

func prob(o float64) float64 { return o / (1 + o) }

// shift moves an anchor probability along a rating's curve.
func shift(anchor float64, rating int, spread float64) float64 {
	return prob(odds(anchor) * mult(rating, spread))
}

// log5 combines a batter rate and a pitcher rate against the league rate.
func log5(b, p, league float64) float64 {
	return prob(odds(b) * odds(p) / odds(league))
}

// plateProbs are the outcome probabilities for one plate appearance.
type plateProbs struct {
	k, bb, hbp float64 // per plate appearance
	hr         float64 // per ball in play
	hit        float64 // per ball in play that is not a home run
	double     float64 // per hit that is not a home run
	triple     float64
}

// matchup computes the plate probabilities for a batter against a pitcher
// in front of a defense, the mean Defense rating of the eight fielders.
func matchup(b *engine.Batting, bats engine.Hand, p *engine.Pitching, throws engine.Hand, defense float64) plateProbs {
	var pp plateProbs
	pp.k = log5(shift(anchorK, b.Contact, 1/spreadK), shift(anchorK, p.Stuff, spreadK), anchorK)
	pp.bb = log5(shift(anchorBB, b.Eye, spreadBB), shift(anchorBB, p.Control, 1/spreadBB), anchorBB)
	pp.hbp = shift(anchorHBP, p.Control, 1/spreadHBP)
	pp.hr = log5(shift(anchorHR, b.Power, spreadHR), shift(anchorHR, p.Movement, 1/spreadHRPitch), anchorHR)
	batterBABIP := prob(odds(anchorBABIP) * mult(b.Contact, spreadBABIPCon) * mult(b.Speed, spreadBABIPSpd))
	pitcherBABIP := prob(odds(anchorBABIP) * mult(p.Movement, 1/spreadBABIPPit) * math.Pow(1/spreadBABIPDef, (defense-50)/25))
	pp.hit = log5(batterBABIP, pitcherBABIP, anchorBABIP)
	pp.double = shift(anchor2B, b.Power, spread2B)
	pp.triple = shift(anchor3B, b.Speed, spread3B)

	// Same-handed matchups favor the pitcher; switch hitters always have the edge.
	f := 1 / platoon
	if bats != engine.Switch && bats == throws {
		f = platoon
	}
	pp.k = prob(odds(pp.k) * f)
	pp.bb = prob(odds(pp.bb) / f)
	pp.hr = prob(odds(pp.hr) / f)
	pp.hit = prob(odds(pp.hit) / math.Sqrt(f))

	if sum := pp.k + pp.bb + pp.hbp; sum > maxThreeTrue {
		scale := maxThreeTrue / sum
		pp.k *= scale
		pp.bb *= scale
		pp.hbp *= scale
	}
	if sum := pp.double + pp.triple; sum > maxExtraBase {
		scale := maxExtraBase / sum
		pp.double *= scale
		pp.triple *= scale
	}
	return pp
}

// stealTry is the chance a runner on first attempts to steal second.
func stealTry(speed int) float64 { return shift(anchorSBTry, speed, spreadSBTry) }

// stealSuccess is the chance the attempt succeeds against the catcher's Defense.
func stealSuccess(speed, catcher int) float64 {
	return prob(odds(anchorSB) * mult(speed, spreadSBRun) * mult(catcher, 1/spreadSBCatch))
}

// advance is a runner's chance to take an extra base, by speed.
func advance(base float64, speed int) float64 { return shift(base, speed, spreadAdvance) }
