// Package engine simulates one baseball game from two clubs' ratings and a
// seed, pitch by pitch. The same inputs always produce the same play-by-play
// log and stat lines, so any game replays exactly, whole or a pitch at a
// time.
package engine

import (
	"fmt"
	"math/rand/v2"
)

// Result is everything one simulated game produced.
type Result struct {
	Version    string                 `json:"version"`
	Seed       Seed                   `json:"seed"`
	Derivation string                 `json:"derivation"`
	Draws      Draws                  `json:"draws,omitempty"`
	Away       Totals                 `json:"away"`
	Home       Totals                 `json:"home"`
	Innings    int                    `json:"innings"`
	Winner     string                 `json:"winner"`
	LineScore  [2][]int               `json:"line_score"`
	Batters    map[string]BatterLine  `json:"batters"`
	Pitchers   map[string]PitcherLine `json:"pitchers"`
	Plays      []Play                 `json:"plays"`
}

// team is one club's in-game state.
type team struct {
	club        *Club
	idx         int // 0 away, 1 home
	next        int // lineup index due up
	batters     map[string]*BatterLine
	pitchers    map[string]*PitcherLine
	used        map[string]bool
	order       []string // pitchers in order of appearance
	cur         stint
	nextOther   int
	defense     float64 // mean Defense of the eight fielders
	catcher     int     // the catcher's Defense
	leadPitcher string  // pitcher of record when the club last took the lead
	losePitcher string  // pitcher charged with the run that gave the lead away
}

func (t *team) batter(id string) *BatterLine {
	if l, ok := t.batters[id]; ok {
		return l
	}
	l := &BatterLine{}
	t.batters[id] = l
	return l
}

func (t *team) pitcher(id string) *PitcherLine {
	if l, ok := t.pitchers[id]; ok {
		return l
	}
	l := &PitcherLine{}
	t.pitchers[id] = l
	return l
}

// game is the state of one simulation between pitches.
type game struct {
	clubs     [2]*team
	seed      Seed
	draws     Draws
	pcg       *rand.PCG  // the whole game's stream in DrawsGame, saved between pitches
	rng       *rand.Rand // draws from pcg in DrawsGame
	inning    int
	top       bool
	bases     [3]*runner
	outs      int
	score     [2]int
	lineScore [2][]int
	plays     []Play
	over      bool
	decided   bool

	balls, strikes int
	halfStart      int       // the batting side's runs when the half began
	pa             *plateCtx // the plate appearance in progress, nil once the game is over
	pitches        int       // pitches thrown
	round          uint64    // DrawsPitch: the round the next pitch needs
	chains         map[[2]string]chain

	// accumulators for the play being resolved
	curRuns    int
	curCharged []string
	curRBI     *BatterLine
}

// plateCtx is the plate appearance in progress.
type plateCtx struct {
	bat, fld *team
	p        Player
	sp       *Player
	pp       plateProbs
	ch       chain
	bl       *BatterLine
	pl       *PitcherLine
	br       *runner
	before   snapshot
	pitches  []Pitch
}

// Simulate plays one game whole, its draws from one stream of the seed. The
// away club bats first.
func Simulate(home, away Club, seed Seed) (Result, error) {
	gm, err := Start(home, away, seed, DrawsGame, 0)
	if err != nil {
		return Result{}, err
	}
	for !gm.Done() {
		if _, err := gm.Pitch(nil); err != nil {
			return Result{}, err
		}
	}
	return gm.Result(), nil
}

// SimulateLive plays one game whole in per-pitch draws, each pitch's
// randomness fetched by round from rounds, the first pitch on first: a
// replay of a game played live.
func SimulateLive(home, away Club, seed Seed, first uint64, rounds func(round uint64) ([]byte, error)) (Result, error) {
	gm, err := Start(home, away, seed, DrawsPitch, first)
	if err != nil {
		return Result{}, err
	}
	for !gm.Done() {
		round, _ := gm.Next()
		randomness, err := rounds(round)
		if err != nil {
			return Result{}, err
		}
		if _, err := gm.Pitch(randomness); err != nil {
			return Result{}, err
		}
	}
	return gm.Result(), nil
}

// checkClubs rejects a club that cannot take the field or a player who
// appears in both.
func checkClubs(home, away Club) error {
	for _, c := range []Club{home, away} {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, p := range home.players() {
		seen[p.ID] = true
	}
	for _, p := range away.players() {
		if seen[p.ID] {
			return fmt.Errorf("engine: player id %q appears in both clubs", p.ID)
		}
	}
	return nil
}

func newTeam(c *Club, idx int) *team {
	t := &team{
		club:     c,
		idx:      idx,
		batters:  map[string]*BatterLine{},
		pitchers: map[string]*PitcherLine{},
		used:     map[string]bool{c.Starter.ID: true},
		order:    []string{c.Starter.ID},
		cur:      stint{pitcher: &c.Starter},
	}
	t.leadPitcher = c.Starter.ID
	t.setDefense()
	return t
}

// setDefense reads the fielders' mean Defense and the catcher's from the club.
func (t *team) setDefense() {
	sum := 0.0
	for _, s := range t.club.Lineup {
		if s.Position == "DH" {
			continue
		}
		sum += float64(s.Player.Batting.Defense)
		if s.Position == "C" {
			t.catcher = s.Player.Batting.Defense
		}
	}
	t.defense = sum / 8
}

func newGame(away, home *Club, seed Seed, draws Draws, first uint64) *game {
	g := &game{seed: seed, draws: draws, inning: 1, top: true, round: first, chains: map[[2]string]chain{}}
	if draws == DrawsGame {
		g.pcg = rand.NewPCG(seed.A, seed.B)
		g.rng = rand.New(g.pcg)
	}
	for i, c := range []*Club{away, home} {
		t := newTeam(c, i)
		for _, p := range c.players() {
			if p.Batting != nil {
				t.batter(p.ID)
			}
		}
		t.pitcher(c.Starter.ID)
		g.clubs[i] = t
	}
	g.beginHalf()
	g.beginPlate()
	return g
}

// beginHalf starts a half-inning: the bases clear, the outs reset, the
// fielding side changes its pitcher when the rules say, and the score is
// noted for the line score.
func (g *game) beginHalf() {
	bat, fld := g.batting(), g.fielding()
	g.bases = [3]*runner{}
	g.outs = 0
	g.halfStart = g.score[bat.idx]
	g.startHalf(fld)
}

// beginPlate starts the next batter's plate appearance, after the starter's
// mid-inning hook when he has reached his limit; it reports a pitcher change.
func (g *game) beginPlate() bool {
	bat, fld := g.batting(), g.fielding()
	was := fld.cur.pitcher.ID
	g.midHalf(fld)
	p := bat.club.Lineup[bat.next].Player
	sp := fld.cur.pitcher
	pp := matchup(p.Batting, p.Bats, sp.Pitching, sp.Throws, fld.defense)
	bl := bat.batter(p.ID)
	bl.PA++
	fld.cur.batters++
	g.balls, g.strikes = 0, 0
	g.pa = &plateCtx{bat: bat, fld: fld, p: p, sp: sp, pp: pp, ch: g.chainFor(p.ID, sp.ID, pp), bl: bl, pl: fld.pitcher(sp.ID),
		br: &runner{id: p.ID, pitcher: sp.ID, speed: p.Batting.Speed}, before: g.snapshot()}
	g.beginPlay(bl)
	return fld.cur.pitcher.ID != was
}

// chainFor is the solved chain for a batter against a pitcher, kept for the game.
func (g *game) chainFor(batter, pitcher string, pp plateProbs) chain {
	key := [2]string{batter, pitcher}
	if c, ok := g.chains[key]; ok {
		return c
	}
	c := solveChain(pp.k, pp.bb)
	g.chains[key] = c
	return c
}

// pitch throws the next pitch with draws from src and resolves what it
// brings: the count, the plate appearance's end, a steal attempt on a pitch
// that is not the last, the half-inning's end, the game's end, and the next
// plate appearance. It answers the pitch and the gap to the next one's round.
func (g *game) pitch(src *rand.Rand) (Pitch, uint64) {
	pa := g.pa
	rec := Pitch{Balls: g.balls, Strikes: g.strikes}
	if g.draws == DrawsPitch {
		rec.Round = g.round
	}
	g.pitches++
	t, b, s, _ := pa.ch.at(g.balls, g.strikes)
	u := src.Float64()
	ended := true
	switch {
	case u < t:
		ip := 1 - pa.pp.k - pa.pp.bb - pa.pp.hbp
		if u/t*(pa.pp.hbp+ip) < pa.pp.hbp {
			rec.Call = CallHitByPitch
			pa.pitches = append(pa.pitches, rec)
			pa.bl.HBP++
			pa.pl.HBP++
			g.force(pa.br)
			g.endPlate(OutcomeHBP)
		} else {
			rec.Call = CallInPlay
			pa.pitches = append(pa.pitches, rec)
			pa.bl.AB++
			g.endPlate(g.inPlay(src, pa.fld, pa.pp, pa.bl, pa.pl, pa.br))
		}
	case u < t+b:
		rec.Call = CallBall
		pa.pitches = append(pa.pitches, rec)
		g.balls++
		if g.balls == 4 {
			pa.bl.BB++
			pa.pl.BB++
			g.force(pa.br)
			g.endPlate(OutcomeBB)
		} else {
			ended = false
		}
	case u < t+b+s:
		rec.Call = CallCalled
		if (u-t-b)/s >= calledShare {
			rec.Call = CallSwinging
		}
		pa.pitches = append(pa.pitches, rec)
		g.strikes++
		if g.strikes == 3 {
			pa.bl.AB++
			pa.bl.K++
			pa.pl.K++
			g.recordOuts(pa.fld, 1)
			g.endPlate(OutcomeK)
		} else {
			ended = false
		}
	default:
		rec.Call = CallFoul
		pa.pitches = append(pa.pitches, rec)
		if g.strikes < 2 {
			g.strikes++
		}
		ended = false
	}
	if !ended && g.bases[0] != nil && g.bases[1] == nil {
		g.steal(src)
		if g.outs >= 3 {
			g.voidPlate()
		}
	}
	return rec, g.afterPitch()
}

// afterPitch ends the half-inning and the game when the pitch did, starts
// the next half-inning and plate appearance, and answers the gap rule's
// rounds to the next pitch.
func (g *game) afterPitch() uint64 {
	gap := uint64(GapPitch)
	if g.runnersOn() > 0 {
		gap = GapRunner
	}
	if g.outs >= 3 || g.over {
		bat := g.batting()
		g.lineScore[bat.idx] = append(g.lineScore[bat.idx], g.score[bat.idx]-g.halfStart)
		switch {
		case g.over:
		case g.top && g.inning >= 9 && g.score[1] > g.score[0]:
			g.over = true
		case g.top:
			g.top = false
		case g.inning >= 9 && g.score[0] != g.score[1]:
			g.over = true
		default:
			g.top = true
			g.inning++
		}
		if g.over {
			g.pa = nil
			g.decide()
			g.decided = true
			return 0
		}
		gap = GapBreak
	}
	if g.pa == nil {
		if gap == GapBreak {
			g.beginHalf()
		}
		if g.beginPlate() {
			gap = GapBreak
		}
	}
	if g.draws == DrawsPitch {
		g.round += gap
	}
	return gap
}

// endPlate ends the plate appearance in progress with its outcome and logs
// it with its pitches; the next batter is due.
func (g *game) endPlate(outcome string) {
	pa := g.pa
	g.endPlay(pa.before, pa.p.ID, pa.sp.ID, outcome, pa.pitches, 0)
	pa.bat.next = (pa.bat.next + 1) % 9
	g.pa = nil
}

// voidPlate ends a plate appearance the third out on the bases interrupted:
// it counts nowhere, its pitches stay in the log on a play with no outcome,
// and the same batter leads off the next half-inning.
func (g *game) voidPlate() {
	pa := g.pa
	pa.bl.PA--
	pa.fld.cur.batters--
	g.endPlay(pa.before, pa.p.ID, pa.sp.ID, "", pa.pitches, 0)
	g.pa = nil
}

// inPlay resolves a ball in play: home run, hit by type, or out by type.
func (g *game) inPlay(src *rand.Rand, fld *team, pp plateProbs, bl *BatterLine, pl *PitcherLine, br *runner) string {
	if src.Float64() < pp.hr {
		bl.HR++
		pl.H++
		g.homer(br)
		return OutcomeHR
	}
	if src.Float64() < pp.hit {
		pl.H++
		r := src.Float64()
		switch {
		case r < pp.triple:
			bl.Triples++
			g.triple(br)
			return Outcome3B
		case r < pp.triple+pp.double:
			bl.Doubles++
			g.double(src, br)
			return Outcome2B
		default:
			bl.Singles++
			g.single(src, br)
			return Outcome1B
		}
	}
	if src.Float64() < anchorGB {
		return g.groundOut(src, fld)
	}
	outcome := g.airOut(src, fld)
	if outcome == OutcomeSF {
		bl.AB--
		bl.SF++
	}
	return outcome
}

// recordOuts charges outs to the fielding team's current pitcher.
func (g *game) recordOuts(fld *team, n int) {
	g.outs += n
	fld.pitcher(fld.cur.pitcher.ID).Outs += n
	fld.cur.outs += n
}

// scoreRun scores a runner, credits and charges the run, and tracks the lead.
func (g *game) scoreRun(r *runner, rbi bool) {
	bat, fld := g.batting(), g.fielding()
	before := g.score[bat.idx] - g.score[fld.idx]
	g.score[bat.idx]++
	bat.batter(r.id).R++
	if rbi && g.curRBI != nil {
		g.curRBI.RBI++
	}
	pl := fld.pitcher(r.pitcher)
	pl.R++
	pl.ER++
	g.curRuns++
	g.curCharged = append(g.curCharged, r.pitcher)
	if before <= 0 && before+1 > 0 {
		bat.leadPitcher = bat.cur.pitcher.ID
		fld.losePitcher = r.pitcher
	}
	if s := &fld.cur; s.saveSpot && !s.blown && before < 0 && before+1 >= 0 {
		s.blown = true
		fld.pitcher(s.pitcher.ID).BS++
	}
	if !g.top && g.inning >= 9 && g.score[1] > g.score[0] {
		g.over = true
	}
}

func (g *game) batting() *team {
	if g.top {
		return g.clubs[0]
	}
	return g.clubs[1]
}

func (g *game) fielding() *team {
	if g.top {
		return g.clubs[1]
	}
	return g.clubs[0]
}

// lead is the fielding team's lead, negative when it trails.
func (g *game) lead(t *team) int { return g.score[t.idx] - g.score[1-t.idx] }

func (g *game) runnersOn() int {
	n := 0
	for _, r := range g.bases {
		if r != nil {
			n++
		}
	}
	return n
}

type snapshot struct {
	bases [3]string
	outs  int
}

func (g *game) snapshot() snapshot {
	var s snapshot
	for i, r := range g.bases {
		if r != nil {
			s.bases[i] = r.id
		}
	}
	s.outs = g.outs
	return s
}

func (g *game) beginPlay(rbi *BatterLine) {
	g.curRuns = 0
	g.curCharged = nil
	g.curRBI = rbi
}

// endPlay logs a play: a plate appearance with its pitches and outcome, an
// interrupted one with its pitches and no outcome, or a steal on the pitch
// numbered onPitch of the plate appearance in progress.
func (g *game) endPlay(before snapshot, batter, pitcher, outcome string, pitches []Pitch, onPitch int) {
	after := g.snapshot()
	half := Top
	if !g.top {
		half = Bottom
	}
	p := Play{
		Inning:      g.inning,
		Half:        half,
		Batter:      batter,
		Pitcher:     pitcher,
		Outcome:     outcome,
		BasesBefore: before.bases,
		OutsBefore:  before.outs,
		BasesAfter:  after.bases,
		OutsAfter:   after.outs,
		Runs:        g.curRuns,
		Charged:     g.curCharged,
		Away:        g.score[0],
		Home:        g.score[1],
		Pitches:     pitches,
		OnPitch:     onPitch,
	}
	if outcome == "" {
		p.Text = fmt.Sprintf("%s %d: %s, inning ended", half, g.inning, batter)
	} else {
		p.Text = fmt.Sprintf("%s %d: %s %s", half, g.inning, batter, outcome)
	}
	if g.curRuns > 0 {
		p.Text += fmt.Sprintf(", %d scored", g.curRuns)
	}
	g.plays = append(g.plays, p)
}

// result assembles the public result from the game state.
func (g *game) result() Result {
	r := Result{
		Version:    Version,
		Seed:       g.seed,
		Derivation: g.seed.Derivation(),
		Draws:      g.draws,
		Innings:    g.inning,
		LineScore:  g.lineScore,
		Batters:    map[string]BatterLine{},
		Pitchers:   map[string]PitcherLine{},
		Plays:      g.plays,
	}
	totals := [2]*Totals{&r.Away, &r.Home}
	for i, t := range g.clubs {
		tot := totals[i]
		tot.Name = t.club.Name
		tot.Runs = g.score[i]
		for id, l := range t.batters {
			r.Batters[id] = *l
			tot.Batting.Add(*l)
		}
		for id, l := range t.pitchers {
			r.Pitchers[id] = *l
			tot.Pitching.Add(*l)
		}
	}
	if g.score[1] > g.score[0] {
		r.Winner = r.Home.Name
	} else {
		r.Winner = r.Away.Name
	}
	return r
}
