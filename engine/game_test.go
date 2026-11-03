package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSameClubsAndSeedReplayExactly(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	var first []byte
	differ := 0
	for i := range uint64(100) {
		seed := DeriveSeed(42, 1, i)
		a, _ := json.Marshal(mustSimulate(t, home, away, seed))
		b, _ := json.Marshal(mustSimulate(t, home, away, seed))
		if string(a) != string(b) {
			t.Fatalf("seed %d: two runs differ", i)
		}
		if i == 0 {
			first = a
			continue
		}
		if string(a) != string(first) {
			differ++
		}
	}
	if differ < 99 {
		t.Errorf("only %d of 99 other seeds produced a different game", differ)
	}
}

func TestResultCarriesVersionSeedAndDerivation(t *testing.T) {
	r := mustSimulate(t, averageClub("Home", "h"), averageClub("Away", "a"), DeriveSeed(1, 2, 3))
	if r.Version != Version || r.Derivation != DerivationSHA || r.Seed.A == 0 {
		t.Errorf("version %q derivation %q seed %+v", r.Version, r.Derivation, r.Seed)
	}
	if r.Away.Name != "Away" || r.Home.Name != "Home" || r.Winner == "" {
		t.Errorf("names %q %q winner %q", r.Away.Name, r.Home.Name, r.Winner)
	}
}

func TestGameRulesHoldAcrossAThousandGames(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	for gi, r := range series(t, home, away, 11, 1000) {
		if r.Winner == "" || r.Away.Runs == r.Home.Runs {
			t.Fatalf("game %d ended %d-%d with winner %q", gi, r.Away.Runs, r.Home.Runs, r.Winner)
		}
		plays := r.Plays
		prevAway, prevHome := 0, 0
		var scoreAfterTopNine, scoreAfterNine *[2]int
		sawBottomNine, maxInning := false, 0
		for i, p := range plays {
			last := i == len(plays)-1 || plays[i+1].Inning != p.Inning || plays[i+1].Half != p.Half
			finalPlay := i == len(plays)-1
			if last && p.OutsAfter != 3 {
				walkoff := finalPlay && p.Half == Bottom && p.Inning >= 9 && p.Home > p.Away
				if !walkoff {
					t.Fatalf("game %d: half %d %s ended with %d outs", gi, p.Inning, p.Half, p.OutsAfter)
				}
				if prevHome > prevAway {
					t.Fatalf("game %d: walk-off while already leading", gi)
				}
			}
			if finalPlay && p.Half == Bottom && p.Inning >= 9 && p.OutsAfter == 3 && p.Home == p.Away {
				t.Fatalf("game %d: ended tied", gi)
			}
			if p.Inning > maxInning {
				maxInning = p.Inning
			}
			if p.Inning == 9 && p.Half == Top && last {
				s := [2]int{p.Away, p.Home}
				scoreAfterTopNine = &s
			}
			if p.Inning == 9 && p.Half == Bottom {
				sawBottomNine = true
				if last {
					s := [2]int{p.Away, p.Home}
					scoreAfterNine = &s
				}
			}
			switch p.Outcome {
			case OutcomeBB, OutcomeHBP:
				if p.BasesAfter[0] != p.Batter {
					t.Fatalf("game %d play %d: walk did not put the batter on first", gi, i)
				}
				if p.BasesBefore[0] != "" && p.BasesAfter[1] != p.BasesBefore[0] {
					t.Fatalf("game %d play %d: walk did not force the runner to second", gi, i)
				}
				loaded := runnersOn(p.BasesBefore) == 3
				if loaded && (p.Runs != 1 || p.BasesAfter[2] != p.BasesBefore[1]) {
					t.Fatalf("game %d play %d: bases-loaded walk scored %d", gi, i, p.Runs)
				}
				if !loaded && p.Runs != 0 {
					t.Fatalf("game %d play %d: walk scored without the bases loaded", gi, i)
				}
			case OutcomeHR:
				if runnersOn(p.BasesAfter) != 0 || p.Runs != runnersOn(p.BasesBefore)+1 {
					t.Fatalf("game %d play %d: home run left runners or scored %d", gi, i, p.Runs)
				}
			}
			if p.Away-prevAway+p.Home-prevHome != p.Runs {
				t.Fatalf("game %d play %d: score moved by other than its runs", gi, i)
			}
			if len(p.Charged) != p.Runs {
				t.Fatalf("game %d play %d: %d runs but %d charged pitchers", gi, i, p.Runs, len(p.Charged))
			}
			prevAway, prevHome = p.Away, p.Home
		}
		if scoreAfterTopNine == nil {
			t.Fatalf("game %d: no top of the ninth", gi)
		}
		homeAheadAfterTop := scoreAfterTopNine[1] > scoreAfterTopNine[0]
		if homeAheadAfterTop == sawBottomNine {
			t.Fatalf("game %d: home ahead after the top of the ninth %v, bottom played %v", gi, homeAheadAfterTop, sawBottomNine)
		}
		if maxInning > 9 && (scoreAfterNine == nil || scoreAfterNine[0] != scoreAfterNine[1]) {
			t.Fatalf("game %d: extra innings without a ninth-inning tie", gi)
		}
		if r.Innings != maxInning {
			t.Fatalf("game %d: result says %d innings, log shows %d", gi, r.Innings, maxInning)
		}
	}
}

func TestStatLinesAgreeWithTheLogAndEachOther(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	starters := [2]string{away.Starter.ID, home.Starter.ID}
	for gi, r := range series(t, home, away, 11, 1000) {
		teams := [2]Totals{r.Away, r.Home}
		for i, tot := range teams {
			opp := teams[1-i]
			b, p := tot.Batting, opp.Pitching
			if b.PA != b.AB+b.BB+b.HBP+b.SF {
				t.Fatalf("game %d %s: PA %d != AB %d + BB %d + HBP %d + SF %d", gi, tot.Name, b.PA, b.AB, b.BB, b.HBP, b.SF)
			}
			if b.Hits() != p.H || b.K != p.K || b.BB != p.BB || b.HBP != p.HBP {
				t.Fatalf("game %d %s: batting and opposing pitching lines disagree", gi, tot.Name)
			}
			if b.R != p.R || p.R != p.ER || tot.Runs != b.R {
				t.Fatalf("game %d %s: runs %d, batter R %d, pitcher R %d, ER %d", gi, tot.Name, tot.Runs, b.R, p.R, p.ER)
			}
			if b.RBI > b.R {
				t.Fatalf("game %d %s: RBI %d exceeds runs %d", gi, tot.Name, b.RBI, b.R)
			}
		}
		outs := [2]int{}
		pas := map[string]int{}
		for _, p := range r.Plays {
			fld := 1
			if p.Half == Bottom {
				fld = 0
			}
			outs[fld] += p.OutsAfter - p.OutsBefore
			if p.Outcome != OutcomeSB && p.Outcome != OutcomeCS {
				pas[p.Batter]++
			}
		}
		if r.Away.Pitching.Outs != outs[0] || r.Home.Pitching.Outs != outs[1] {
			t.Fatalf("game %d: pitcher outs %d/%d, log outs %d/%d", gi, r.Away.Pitching.Outs, r.Home.Pitching.Outs, outs[0], outs[1])
		}
		for id, l := range r.Batters {
			if l.PA != pas[id] {
				t.Fatalf("game %d: batter %s has PA %d, log shows %d", gi, id, l.PA, pas[id])
			}
		}
		w, l, sv := 0, 0, 0
		for id, pl := range r.Pitchers {
			w += pl.W
			l += pl.L
			sv += pl.SV
			if pl.QS == 1 && (pl.Outs < qualityOuts || pl.ER > qualityER) {
				t.Fatalf("game %d: %s has a quality start with %d outs and %d ER", gi, id, pl.Outs, pl.ER)
			}
		}
		if w != 1 || l != 1 || sv > 1 {
			t.Fatalf("game %d: W %d L %d SV %d", gi, w, l, sv)
		}
		want := deriveDecisions(r, starters)
		for id, pl := range r.Pitchers {
			d := want[id]
			got := [8]int{pl.W, pl.L, pl.SV, pl.HLD, pl.BS, pl.QS, pl.ER, pl.Outs}
			exp := [8]int{d.W, d.L, d.SV, d.HLD, d.BS, d.QS, d.ER, d.Outs}
			if got != exp {
				t.Fatalf("game %d: %s decisions W L SV HLD BS QS ER Outs = %v, log says %v", gi, id, got, exp)
			}
		}
	}
}

// stintRec is one pitcher's outing as read back from the log.
type stintRec struct {
	pitcher      string
	team         int
	entryLead    int
	entryRunners int
	outs         int
	exitLead     int
	finished     bool
	blown        bool
}

// deriveDecisions re-derives every pitcher decision from the play-by-play
// log alone, independently of the engine's bookkeeping.
func deriveDecisions(r Result, starters [2]string) map[string]PitcherLine {
	lines := map[string]*PitcherLine{}
	get := func(id string) *PitcherLine {
		if l, ok := lines[id]; ok {
			return l
		}
		l := &PitcherLine{}
		lines[id] = l
		return l
	}
	var stints [2][]*stintRec
	var cur [2]*stintRec
	for i, sp := range starters {
		cur[i] = &stintRec{pitcher: sp, team: i}
		stints[i] = []*stintRec{cur[i]}
	}
	lastPitcher := starters
	var leadPitcher, losePitcher [2]string
	prev := [2]int{}
	for _, p := range r.Plays {
		fld := 1
		if p.Half == Bottom {
			fld = 0
		}
		bat := 1 - fld
		leadBefore := prev[fld] - prev[bat]
		if cur[fld] == nil || cur[fld].pitcher != p.Pitcher {
			if cur[fld] != nil {
				cur[fld].exitLead = leadBefore
			}
			cur[fld] = &stintRec{pitcher: p.Pitcher, team: fld, entryLead: leadBefore, entryRunners: runnersOn(p.BasesBefore)}
			stints[fld] = append(stints[fld], cur[fld])
		}
		s := cur[fld]
		s.outs += p.OutsAfter - p.OutsBefore
		get(p.Pitcher).Outs += p.OutsAfter - p.OutsBefore
		for _, c := range p.Charged {
			get(c).ER++
		}
		leadAfter := leadBefore - p.Runs
		if saveSituation(s.entryLead, s.entryRunners) && leadBefore > 0 && leadAfter <= 0 {
			s.blown = true
		}
		if leadBefore >= 0 && leadAfter < 0 {
			leadPitcher[bat] = lastPitcher[bat]
			losePitcher[fld] = p.Charged[leadBefore]
		}
		lastPitcher[fld] = p.Pitcher
		prev = [2]int{p.Away, p.Home}
	}
	for i := range cur {
		if cur[i] != nil {
			cur[i].finished = true
			cur[i].exitLead = prev[i] - prev[1-i]
		}
	}
	w := 1
	if r.Away.Runs > r.Home.Runs {
		w = 0
	}
	l := 1 - w
	winner := leadPitcher[w]
	if winner == starters[w] && get(starters[w]).Outs < starterWinOuts && len(stints[w]) > 1 {
		winner = stints[w][1].pitcher
	}
	get(winner).W++
	get(losePitcher[l]).L++
	fin := stints[w][len(stints[w])-1]
	if fin.pitcher != winner && fin.pitcher != starters[w] && fin.entryLead > 0 && fin.outs >= 1 &&
		((fin.entryLead <= saveLeadMax && fin.outs >= relieverOuts) || fin.entryLead <= fin.entryRunners+2 || fin.outs >= saveLongOuts) {
		get(fin.pitcher).SV++
	}
	for _, team := range stints {
		for _, s := range team {
			if s.pitcher != starters[s.team] && saveSituation(s.entryLead, s.entryRunners) && s.outs >= 1 && !s.finished && s.exitLead > 0 {
				get(s.pitcher).HLD++
			}
			if s.blown {
				get(s.pitcher).BS++
			}
		}
	}
	for _, sp := range starters {
		if pl := get(sp); pl.Outs >= qualityOuts && pl.ER <= qualityER {
			pl.QS++
		}
	}
	out := map[string]PitcherLine{}
	for id, pl := range lines {
		out[id] = *pl
	}
	return out
}

func TestRatingsMoveGameOutcomes(t *testing.T) {
	const n = 500
	away := averageClub("Away", "a")
	sumHome := func(rs []Result, f func(Result) int) int {
		total := 0
		for _, r := range rs {
			total += f(r)
		}
		return total
	}
	homeHR := func(r Result) int { return r.Home.Batting.HR }
	strong := sumHome(series(t, ratedClub("Home", "h", Batting{50, 75, 50, 50, 50}, avgPit), away, 21, n), homeHR)
	weak := sumHome(series(t, ratedClub("Home", "h", Batting{50, 25, 50, 50, 50}, avgPit), away, 21, n), homeHR)
	if strong <= weak {
		t.Errorf("power 75 hit %d home runs, power 25 hit %d", strong, weak)
	}
	homeK := func(r Result) int { return r.Home.Pitching.K }
	strong = sumHome(series(t, ratedClub("Home", "h", avgBat, Pitching{75, 50, 50, 50}), away, 22, n), homeK)
	weak = sumHome(series(t, ratedClub("Home", "h", avgBat, Pitching{25, 50, 50, 50}), away, 22, n), homeK)
	if strong <= weak {
		t.Errorf("stuff 75 struck out %d, stuff 25 struck out %d", strong, weak)
	}
	homeSB := func(r Result) int { return r.Home.Batting.SB }
	homeTries := func(r Result) int { return r.Home.Batting.SB + r.Home.Batting.CS }
	fast := series(t, ratedClub("Home", "h", Batting{50, 50, 50, 75, 50}, avgPit), away, 23, n)
	slow := series(t, ratedClub("Home", "h", Batting{50, 50, 50, 25, 50}, avgPit), away, 23, n)
	if sumHome(fast, homeTries) <= sumHome(slow, homeTries) || sumHome(fast, homeSB) <= sumHome(slow, homeSB) {
		t.Errorf("speed 75 tried %d and stole %d, speed 25 tried %d and stole %d",
			sumHome(fast, homeTries), sumHome(fast, homeSB), sumHome(slow, homeTries), sumHome(slow, homeSB))
	}
	awayBABIP := func(rs []Result) float64 {
		var b BatterLine
		for _, r := range rs {
			b.Add(r.Away.Batting)
		}
		return float64(b.Hits()-b.HR) / float64(b.AB-b.K-b.HR+b.SF)
	}
	good := awayBABIP(series(t, ratedClub("Home", "h", Batting{50, 50, 50, 50, 75}, avgPit), away, 24, n))
	bad := awayBABIP(series(t, ratedClub("Home", "h", Batting{50, 50, 50, 50, 25}, avgPit), away, 24, n))
	if good >= bad {
		t.Errorf("defense 75 allowed BABIP %.3f, defense 25 allowed %.3f", good, bad)
	}
	onBase := func(rs []Result) float64 {
		var b BatterLine
		for _, r := range rs {
			b.Add(r.Home.Batting)
		}
		return float64(b.Hits()+b.BB+b.HBP) / float64(b.PA)
	}
	rightPitchers := handedClub("Away", "a", avgBat, avgPit, nil, []Hand{Right})
	same := onBase(series(t, handedClub("Home", "h", avgBat, avgPit, []Hand{Right}, nil), rightPitchers, 25, n))
	opposite := onBase(series(t, handedClub("Home", "h", avgBat, avgPit, []Hand{Left}, nil), rightPitchers, 25, n))
	if same >= opposite {
		t.Errorf("same-handed on-base %.3f, opposite-handed %.3f", same, opposite)
	}
}

// golden pins the fixed game for each engine version.
var golden = map[string]struct {
	away, home int
	digest     string
}{
	"2": {3, 4, "dc9a57ffff45818f99ddd650f638cb37ef0d29ac86b7a8cde7c04813dc56e6e1"},
}

func TestGoldenGameIsPinnedForThisVersion(t *testing.T) {
	home := ratedClub("Mudville", "m", Batting{60, 55, 45, 70, 50}, Pitching{55, 45, 60, 65})
	away := ratedClub("Visitors", "v", Batting{45, 65, 60, 40, 55}, Pitching{60, 60, 40, 50})
	r := mustSimulate(t, home, away, DeriveSeed(2026, 1, 1))
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	want, ok := golden[Version]
	if !ok {
		t.Fatalf("no golden entry for engine version %q; add one with score %d-%d and digest %s", Version, r.Away.Runs, r.Home.Runs, digest)
	}
	if want.away != r.Away.Runs || want.home != r.Home.Runs || want.digest != digest {
		t.Fatalf("golden game for version %q changed: score %d-%d digest %s; pinned %d-%d %s",
			Version, r.Away.Runs, r.Home.Runs, digest, want.away, want.home, want.digest)
	}
}
