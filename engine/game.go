// Package engine simulates one baseball game from two clubs' ratings and a
// seed. The same inputs always produce the same play-by-play log and stat
// lines, so any game replays exactly.
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
	next        int // next lineup slot to bat
	batters     map[string]*BatterLine
	pitchers    map[string]*PitcherLine
	cur         stint
	used        map[string]bool
	order       []string // pitchers in order of appearance
	nextOther   int
	leadPitcher string // pitcher of record when this team last took the lead
	losePitcher string // pitcher charged with the last go-ahead run against this team
	defense     float64
	catcher     int
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

// game is the state of one simulation.
type game struct {
	clubs     [2]*team
	seed      Seed
	rng       *rand.Rand
	inning    int
	top       bool
	bases     [3]*runner
	outs      int
	score     [2]int
	lineScore [2][]int
	plays     []Play
	over      bool

	// accumulators for the play being resolved
	curRuns    int
	curCharged []string
	curRBI     *BatterLine
}

// Simulate plays one game. The away club bats first.
func Simulate(home, away Club, seed Seed) (Result, error) {
	for _, c := range []Club{home, away} {
		if err := c.Validate(); err != nil {
			return Result{}, err
		}
	}
	seen := map[string]bool{}
	for _, p := range home.players() {
		seen[p.ID] = true
	}
	for _, p := range away.players() {
		if seen[p.ID] {
			return Result{}, fmt.Errorf("engine: player id %q appears in both clubs", p.ID)
		}
	}
	g := newGame(&away, &home, seed)
	g.play()
	g.decide()
	return g.result(), nil
}

func newGame(away, home *Club, seed Seed) *game {
	g := &game{seed: seed, rng: seed.stream()}
	for i, c := range []*Club{away, home} {
		t := &team{
			club:     c,
			idx:      i,
			batters:  map[string]*BatterLine{},
			pitchers: map[string]*PitcherLine{},
			used:     map[string]bool{c.Starter.ID: true},
			order:    []string{c.Starter.ID},
			cur:      stint{pitcher: &c.Starter},
		}
		t.leadPitcher = c.Starter.ID
		sum := 0.0
		for _, s := range c.Lineup {
			if s.Position == "DH" {
				continue
			}
			sum += float64(s.Player.Batting.Defense)
			if s.Position == "C" {
				t.catcher = s.Player.Batting.Defense
			}
		}
		t.defense = sum / 8
		for _, p := range c.players() {
			if p.Batting != nil {
				t.batter(p.ID)
			}
		}
		t.pitcher(c.Starter.ID)
		g.clubs[i] = t
	}
	return g
}

// play runs innings until one side leads after a completed inning from the
// ninth on, or a walk-off ends the game.
func (g *game) play() {
	for g.inning = 1; !g.over; g.inning++ {
		g.top = true
		g.half(g.clubs[0], g.clubs[1])
		if g.over {
			return
		}
		if g.inning >= 9 && g.score[1] > g.score[0] {
			g.over = true
			return
		}
		g.top = false
		g.half(g.clubs[1], g.clubs[0])
		if g.over {
			return
		}
		if g.inning >= 9 && g.score[0] != g.score[1] {
			g.over = true
			return
		}
	}
}

// half plays one half-inning with bat at the plate and fld in the field.
func (g *game) half(bat, fld *team) {
	g.bases = [3]*runner{}
	g.outs = 0
	start := g.score[bat.idx]
	g.startHalf(fld)
	for g.outs < 3 && !g.over {
		g.midHalf(fld)
		slot := bat.club.Lineup[bat.next]
		if g.bases[0] != nil && g.bases[1] == nil && g.steal(bat, fld, slot.Player) && g.outs >= 3 {
			break
		}
		g.plate(bat, fld, slot.Player)
		bat.next = (bat.next + 1) % 9
	}
	g.lineScore[bat.idx] = append(g.lineScore[bat.idx], g.score[bat.idx]-start)
}

// plate resolves one plate appearance.
func (g *game) plate(bat, fld *team, p Player) {
	sp := fld.cur.pitcher
	pp := matchup(p.Batting, p.Bats, sp.Pitching, sp.Throws, fld.defense)
	bl := bat.batter(p.ID)
	pl := fld.pitcher(sp.ID)
	bl.PA++
	fld.cur.batters++
	before := g.snapshot()
	g.beginPlay(bl)
	br := &runner{id: p.ID, pitcher: sp.ID, speed: p.Batting.Speed}
	var outcome string
	r := g.rng.Float64()
	switch {
	case r < pp.k:
		outcome = OutcomeK
		bl.AB++
		bl.K++
		pl.K++
		g.recordOuts(fld, 1)
	case r < pp.k+pp.bb:
		outcome = OutcomeBB
		bl.BB++
		pl.BB++
		g.force(br)
	case r < pp.k+pp.bb+pp.hbp:
		outcome = OutcomeHBP
		bl.HBP++
		pl.HBP++
		g.force(br)
	default:
		bl.AB++
		outcome = g.inPlay(fld, pp, bl, pl, br)
	}
	g.endPlay(before, p.ID, sp.ID, outcome)
}

// inPlay resolves a ball in play: home run, hit by type, or out by type.
func (g *game) inPlay(fld *team, pp plateProbs, bl *BatterLine, pl *PitcherLine, br *runner) string {
	if g.rng.Float64() < pp.hr {
		bl.HR++
		pl.H++
		g.homer(br)
		return OutcomeHR
	}
	if g.rng.Float64() < pp.hit {
		pl.H++
		r := g.rng.Float64()
		switch {
		case r < pp.triple:
			bl.Triples++
			g.triple(br)
			return Outcome3B
		case r < pp.triple+pp.double:
			bl.Doubles++
			g.double(br)
			return Outcome2B
		default:
			bl.Singles++
			g.single(br)
			return Outcome1B
		}
	}
	if g.rng.Float64() < anchorGB {
		return g.groundOut(fld)
	}
	outcome := g.airOut(fld)
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

func (g *game) endPlay(before snapshot, batter, pitcher, outcome string) {
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

		Text: fmt.Sprintf("%s %d: %s %s", half, g.inning, batter, outcome)}
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
