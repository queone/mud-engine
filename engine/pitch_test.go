package engine

import (
	"math"
	"testing"
)

// The ratings the pitch model is checked at: average, 25 points either way,
// and the extremes.
var ratingLevels = []struct {
	name string
	b    Batting
	p    Pitching
}{
	{"average", avgBat, avgPit},
	{"batters up 25", Batting{Contact: 75, Power: 75, Eye: 75, Speed: 75, Defense: 75}, Pitching{Stuff: 25, Control: 25, Movement: 25, Stamina: 50}},
	{"pitchers up 25", Batting{Contact: 25, Power: 25, Eye: 25, Speed: 25, Defense: 25}, Pitching{Stuff: 75, Control: 75, Movement: 75, Stamina: 50}},
	{"batters at 99", Batting{Contact: 99, Power: 99, Eye: 99, Speed: 99, Defense: 99}, Pitching{Stuff: 1, Control: 1, Movement: 1, Stamina: 50}},
	{"pitchers at 99", Batting{Contact: 1, Power: 1, Eye: 1, Speed: 1, Defense: 1}, Pitching{Stuff: 99, Control: 99, Movement: 99, Stamina: 50}},
}

// rightClub is a club of one rating with every batter and pitcher
// right-handed, so every plate appearance has one matchup.
func rightClub(name, prefix string, b Batting, p Pitching) Club {
	return handedClub(name, prefix, b, p, []Hand{Right}, []Hand{Right})
}

// The chain a matchup solves for ends its plate appearances in a strikeout,
// a walk, a hit by pitch, and a ball in play at the matchup's own chances, at
// every rating level, to within a hundred-millionth; and an average matchup
// expects between 3.7 and 4.1 pitches.
func TestTheChainSolvesEachMatchupsTargets(t *testing.T) {
	for _, lv := range ratingLevels {
		pp := matchup(&lv.b, Right, &lv.p, Right, float64(lv.b.Defense))
		c := solveChain(pp.k, pp.bb)
		k, bb, pitches := c.absorb()
		terminal := 1 - k - bb
		ip := 1 - pp.k - pp.bb - pp.hbp
		hbp := terminal * pp.hbp / (pp.hbp + ip)
		for _, x := range []struct {
			name      string
			got, want float64
		}{{"strikeout", k, pp.k}, {"walk", bb, pp.bb}, {"hit by pitch", hbp, pp.hbp}, {"in play", terminal - hbp, ip}} {
			if math.Abs(x.got-x.want) > 1e-8 {
				t.Errorf("%s: %s chance %.9f, target %.9f", lv.name, x.name, x.got, x.want)
			}
		}
		if lv.name == "average" && (pitches < 3.7 || pitches > 4.1) {
			t.Errorf("an average plate appearance expects %.2f pitches, want 3.7 to 4.1", pitches)
		}
		t.Logf("%s: K %.4f BB %.4f HBP %.4f, %.2f pitches", lv.name, k, bb, hbp, pitches)
	}
}

// Plate appearances played through the game end as the matchup's targets
// say, within 5 percent or sampling noise, whichever is wider, at every
// rating level, and an average game throws 3.7 to 4.1 pitches a plate
// appearance; a plate appearance the third out on the bases interrupted
// counts nowhere.
func TestPlateAppearancesEmergeWithinFivePercent(t *testing.T) {
	for i, lv := range ratingLevels {
		home, away := rightClub("Home", "h", lv.b, lv.p), rightClub("Away", "a", lv.b, lv.p)
		pp := matchup(&lv.b, Right, &lv.p, Right, float64(lv.b.Defense))
		n := 1200
		if i == 0 {
			n = 3000
		}
		var bat BatterLine
		pitches, pas := 0, 0
		for _, r := range series(t, home, away, uint64(31+i), n) {
			bat.Add(r.Away.Batting)
			bat.Add(r.Home.Batting)
			for _, p := range r.Plays {
				pitches += len(p.Pitches)
				if p.Outcome != "" && p.Outcome != OutcomeSB && p.Outcome != OutcomeCS {
					pas++
				}
			}
		}
		pa := float64(bat.PA)
		if int(pa) != pas {
			t.Fatalf("%s: lines count %d plate appearances, the log %d", lv.name, bat.PA, pas)
		}
		for _, x := range []struct {
			name      string
			got, want float64
		}{{"strikeout", float64(bat.K) / pa, pp.k}, {"walk", float64(bat.BB) / pa, pp.bb}, {"hit by pitch", float64(bat.HBP) / pa, pp.hbp},
			{"in play", float64(bat.PA-bat.K-bat.BB-bat.HBP) / pa, 1 - pp.k - pp.bb - pp.hbp}} {
			noise := 3 * math.Sqrt(x.want*(1-x.want)/pa)
			if math.Abs(x.got-x.want) > math.Max(0.05*x.want, noise) {
				t.Errorf("%s: %s rate %.4f, target %.4f, over %d plate appearances", lv.name, x.name, x.got, x.want, bat.PA)
			}
		}
		if per := float64(pitches) / pa; i == 0 && (per < 3.7 || per > 4.1) {
			t.Errorf("average games threw %.2f pitches a plate appearance, want 3.7 to 4.1", per)
		}
	}
}

// Every pitch is called one of the six ways, the count never reaches four
// balls or three strikes, a foul with two strikes keeps the count, a plate
// appearance ends only on a terminal pitch, a fourth ball, or a third strike,
// a steal rides a pitch that did not end it, and a voided plate appearance
// keeps its pitches, resets the count, and sends the same batter up first.
func TestPitchesFollowTheCountRules(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	voided := 0
	for gi, r := range series(t, home, away, 41, 400) {
		for i, p := range r.Plays {
			if p.Outcome == OutcomeSB || p.Outcome == OutcomeCS {
				if p.OnPitch < 1 || len(p.Pitches) != 0 {
					t.Fatalf("game %d play %d: a steal on pitch %d with %d pitches", gi, i, p.OnPitch, len(p.Pitches))
				}
				continue
			}
			if len(p.Pitches) == 0 {
				t.Fatalf("game %d play %d: a plate appearance with no pitches", gi, i)
			}
			balls, strikes := 0, 0
			for j, pt := range p.Pitches {
				if pt.Balls != balls || pt.Strikes != strikes || pt.Round != 0 {
					t.Fatalf("game %d play %d pitch %d: count %d-%d, want %d-%d, round %d", gi, i, j, pt.Balls, pt.Strikes, balls, strikes, pt.Round)
				}
				last := j == len(p.Pitches)-1
				switch pt.Call {
				case CallBall:
					balls++
				case CallCalled, CallSwinging:
					strikes++
				case CallFoul:
					if strikes < 2 {
						strikes++
					}
				case CallInPlay, CallHitByPitch:
					if !last {
						t.Fatalf("game %d play %d: pitch %d was %s but not the last", gi, i, j, pt.Call)
					}
				default:
					t.Fatalf("game %d play %d pitch %d: call %q", gi, i, j, pt.Call)
				}
				if balls > 4 || strikes > 3 || (balls == 4 || strikes == 3) && !last {
					t.Fatalf("game %d play %d: the count reached %d-%d before the last pitch", gi, i, balls, strikes)
				}
			}
			final := p.Pitches[len(p.Pitches)-1].Call
			switch p.Outcome {
			case OutcomeBB:
				if balls != 4 {
					t.Fatalf("game %d play %d: a walk on %d balls", gi, i, balls)
				}
			case OutcomeK:
				if strikes != 3 {
					t.Fatalf("game %d play %d: a strikeout on %d strikes", gi, i, strikes)
				}
			case OutcomeHBP:
				if final != CallHitByPitch {
					t.Fatalf("game %d play %d: a hit by pitch ended on %s", gi, i, final)
				}
			case "":
				voided++
				if balls == 4 || strikes == 3 || final == CallInPlay || final == CallHitByPitch {
					t.Fatalf("game %d play %d: a voided plate appearance ended on %s at %d-%d", gi, i, final, balls, strikes)
				}
				prev := r.Plays[i-1]
				if prev.Outcome != OutcomeCS || prev.OutsAfter != 3 || p.OutsAfter != 3 {
					t.Fatalf("game %d play %d: a plate appearance voided without a third out on the bases", gi, i)
				}
				for _, next := range r.Plays[i+1:] {
					if next.Half != p.Half || next.Outcome == OutcomeSB || next.Outcome == OutcomeCS {
						continue
					}
					if next.Batter != p.Batter || next.Inning == p.Inning || next.Pitches[0].Balls != 0 || next.Pitches[0].Strikes != 0 {
						t.Fatalf("game %d play %d: after a voided plate appearance %s led off %s %d at %d-%d", gi, i, next.Batter, next.Half, next.Inning, next.Pitches[0].Balls, next.Pitches[0].Strikes)
					}
					break
				}
			default:
				if final != CallInPlay {
					t.Fatalf("game %d play %d: %s ended on %s", gi, i, p.Outcome, final)
				}
			}
		}
	}
	if voided == 0 {
		t.Error("no plate appearance was voided by a third out on the bases in 400 games")
	}
}
