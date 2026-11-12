package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
)

// Game is a game in progress, played a pitch at a time: started with Start,
// or resumed from a State with Resume. Next names the beacon round the next
// pitch needs, Pitch throws it, Done says the game is over, Result reads the
// result so far, and State saves the game between pitches.
type Game struct {
	g          *game
	home, away Club
}

// Start begins a game: the away club bats first. In per-pitch draws, first is
// the round the first pitch draws on, the commitment's bound round; the game
// mode ignores it.
func Start(home, away Club, seed Seed, draws Draws, first uint64) (*Game, error) {
	if draws != DrawsGame && draws != DrawsPitch {
		return nil, fmt.Errorf("engine: draws %q is neither %q nor %q", draws, DrawsGame, DrawsPitch)
	}
	if err := checkClubs(home, away); err != nil {
		return nil, err
	}
	gm := &Game{home: home, away: away}
	gm.g = newGame(&gm.away, &gm.home, seed, draws, first)
	return gm, nil
}

// Next answers the round the next pitch draws on in per-pitch draws, and
// whether a pitch remains; in game draws the round is 0.
func (gm *Game) Next() (uint64, bool) {
	if gm.g.over {
		return 0, false
	}
	if gm.g.draws == DrawsPitch {
		return gm.g.round, true
	}
	return 0, true
}

// Pitch throws the next pitch. In per-pitch draws randomness is the round's,
// the one Next named; in game draws it is ignored. It answers the pitch.
func (gm *Game) Pitch(randomness []byte) (Pitch, error) {
	g := gm.g
	if g.over {
		return Pitch{}, errors.New("engine: the game is over")
	}
	src := g.rng
	if g.draws == DrawsPitch {
		if len(randomness) == 0 {
			return Pitch{}, fmt.Errorf("engine: pitch %d needs round %d's randomness", g.pitches+1, g.round)
		}
		src = pitchStream(g.seed, randomness, g.pitches)
	}
	p, _ := g.pitch(src)
	return p, nil
}

// Done says the game is over.
func (gm *Game) Done() bool { return gm.g.over }

// Pitches is how many pitches have been thrown.
func (gm *Game) Pitches() int { return gm.g.pitches }

// Result is the game's result, final once Done, else the lines and plays so far.
func (gm *Game) Result() Result { return gm.g.result() }

// State is a game between pitches, as JSON saves it: everything the game
// changed, without the clubs, which Resume takes again.
type State struct {
	Version    string       `json:"version"`
	Draws      Draws        `json:"draws"`
	Seed       Seed         `json:"seed"`
	Derivation string       `json:"derivation"`
	Inning     int          `json:"inning"`
	Top        bool         `json:"top"`
	Outs       int          `json:"outs"`
	Balls      int          `json:"balls"`
	Strikes    int          `json:"strikes"`
	Bases      [3]Base      `json:"bases"`
	Score      [2]int       `json:"score"`
	LineScore  [2][]int     `json:"line_score"`
	HalfStart  int          `json:"half_start"`
	Plays      []Play       `json:"plays"`
	Over       bool         `json:"over"`
	Pitches    int          `json:"pitches"`
	Round      uint64       `json:"round,omitempty"`  // per-pitch draws: the next pitch's round
	Stream     []byte       `json:"stream,omitempty"` // game draws: the stream's state
	Teams      [2]TeamState `json:"teams"`
	Plate      *PlateState  `json:"plate,omitempty"`
}

// Base is a runner on base: the player and the pitcher charged if he scores.
type Base struct {
	ID      string `json:"id,omitempty"`
	Pitcher string `json:"pitcher,omitempty"`
}

// TeamState is one club's in-game state.
type TeamState struct {
	Next        int                    `json:"next"`
	NextOther   int                    `json:"next_other"`
	Used        []string               `json:"used"`
	Order       []string               `json:"order"`
	LeadPitcher string                 `json:"lead_pitcher"`
	LosePitcher string                 `json:"lose_pitcher"`
	Cur         StintState             `json:"cur"`
	Batters     map[string]BatterLine  `json:"batters"`
	Pitchers    map[string]PitcherLine `json:"pitchers"`
}

// StintState is the current pitcher's outing.
type StintState struct {
	Pitcher      string `json:"pitcher"`
	Outs         int    `json:"outs"`
	Batters      int    `json:"batters"`
	EntryLead    int    `json:"entry_lead"`
	EntryRunners int    `json:"entry_runners"`
	SaveSpot     bool   `json:"save_spot"`
	Blown        bool   `json:"blown"`
}

// PlateState is the plate appearance in progress: its pitches so far and the
// bases and outs its play began from.
type PlateState struct {
	Pitches     []Pitch   `json:"pitches"`
	BeforeBases [3]string `json:"before_bases"`
	BeforeOuts  int       `json:"before_outs"`
}

// State saves the game between pitches.
func (gm *Game) State() State {
	g := gm.g
	st := State{Version: Version, Draws: g.draws, Seed: g.seed, Derivation: g.seed.Derivation(), Inning: g.inning, Top: g.top, Outs: g.outs,
		Balls: g.balls, Strikes: g.strikes, Score: g.score, LineScore: g.lineScore, HalfStart: g.halfStart, Plays: g.plays, Over: g.over, Pitches: g.pitches}
	for i, r := range g.bases {
		if r != nil {
			st.Bases[i] = Base{ID: r.id, Pitcher: r.pitcher}
		}
	}
	if g.draws == DrawsPitch {
		st.Round = g.round
	} else if !g.over {
		st.Stream, _ = g.pcg.MarshalBinary()
	}
	for i, t := range g.clubs {
		ts := TeamState{Next: t.next, NextOther: t.nextOther, Order: t.order, LeadPitcher: t.leadPitcher, LosePitcher: t.losePitcher,
			Batters: map[string]BatterLine{}, Pitchers: map[string]PitcherLine{},
			Cur: StintState{Pitcher: t.cur.pitcher.ID, Outs: t.cur.outs, Batters: t.cur.batters, EntryLead: t.cur.entryLead, EntryRunners: t.cur.entryRunners, SaveSpot: t.cur.saveSpot, Blown: t.cur.blown}}
		for _, id := range t.order {
			if t.used[id] {
				ts.Used = append(ts.Used, id)
			}
		}
		for id, l := range t.batters {
			ts.Batters[id] = *l
		}
		for id, l := range t.pitchers {
			ts.Pitchers[id] = *l
		}
		st.Teams[i] = ts
	}
	if pa := g.pa; pa != nil {
		st.Plate = &PlateState{Pitches: pa.pitches, BeforeBases: pa.before.bases, BeforeOuts: pa.before.outs}
	}
	return st
}

// MarshalJSON saves the state as JSON.
func (gm *Game) MarshalJSON() ([]byte, error) { return json.Marshal(gm.State()) }

// Resume continues a game from a saved state and the clubs it was started
// with, the committed inputs. A state from another engine model is refused.
func Resume(st State, home, away Club) (*Game, error) {
	if st.Version != Version {
		return nil, fmt.Errorf("engine: the state is from engine model %q; this is model %s", st.Version, Version)
	}
	if st.Draws != DrawsGame && st.Draws != DrawsPitch {
		return nil, fmt.Errorf("engine: draws %q is neither %q nor %q", st.Draws, DrawsGame, DrawsPitch)
	}
	if err := checkClubs(home, away); err != nil {
		return nil, err
	}
	gm := &Game{home: home, away: away}
	g := &game{seed: Seed{A: st.Seed.A, B: st.Seed.B, derivation: st.Derivation}, draws: st.Draws, inning: st.Inning, top: st.Top, outs: st.Outs,
		balls: st.Balls, strikes: st.Strikes, score: st.Score, lineScore: st.LineScore, halfStart: st.HalfStart, plays: st.Plays, over: st.Over, decided: st.Over,
		pitches: st.Pitches, round: st.Round, chains: map[[2]string]chain{}}
	if st.Draws == DrawsGame && !st.Over {
		pcg := rand.NewPCG(0, 0)
		if err := pcg.UnmarshalBinary(st.Stream); err != nil {
			return nil, fmt.Errorf("engine: the state's stream: %w", err)
		}
		g.pcg, g.rng = pcg, rand.New(pcg)
	}
	players := map[string]Player{}
	for _, c := range []*Club{&gm.away, &gm.home} {
		for _, p := range c.players() {
			if q, ok := players[p.ID]; !ok || q.Batting == nil {
				players[p.ID] = p // a two-way player's batting entry wins
			}
		}
	}
	for i, c := range []*Club{&gm.away, &gm.home} {
		ts := st.Teams[i]
		t := &team{club: c, idx: i, next: ts.Next, nextOther: ts.NextOther, used: map[string]bool{}, order: ts.Order, leadPitcher: ts.LeadPitcher, losePitcher: ts.LosePitcher,
			batters: map[string]*BatterLine{}, pitchers: map[string]*PitcherLine{}}
		t.setDefense()
		for _, id := range ts.Used {
			t.used[id] = true
		}
		for id, l := range ts.Batters {
			t.batters[id] = &l
		}
		for id, l := range ts.Pitchers {
			t.pitchers[id] = &l
		}
		sp := c.pitcherNamed(ts.Cur.Pitcher)
		if sp == nil {
			return nil, fmt.Errorf("engine: the state names pitcher %q, not one of %s's", ts.Cur.Pitcher, c.Name)
		}
		t.cur = stint{pitcher: sp, outs: ts.Cur.Outs, batters: ts.Cur.Batters, entryLead: ts.Cur.EntryLead, entryRunners: ts.Cur.EntryRunners, saveSpot: ts.Cur.SaveSpot, blown: ts.Cur.Blown}
		g.clubs[i] = t
	}
	for i, b := range st.Bases {
		if b.ID == "" {
			continue
		}
		p, ok := players[b.ID]
		if !ok || p.Batting == nil {
			return nil, fmt.Errorf("engine: the state puts %q on base, not a batter of either club", b.ID)
		}
		g.bases[i] = &runner{id: b.ID, pitcher: b.Pitcher, speed: p.Batting.Speed}
	}
	if st.Plate != nil && !st.Over {
		bat, fld := g.batting(), g.fielding()
		p := bat.club.Lineup[bat.next].Player
		sp := fld.cur.pitcher
		pp := matchup(p.Batting, p.Bats, sp.Pitching, sp.Throws, fld.defense)
		bl := bat.batter(p.ID)
		g.pa = &plateCtx{bat: bat, fld: fld, p: p, sp: sp, pp: pp, ch: g.chainFor(p.ID, sp.ID, pp), bl: bl, pl: fld.pitcher(sp.ID),
			br: &runner{id: p.ID, pitcher: sp.ID, speed: p.Batting.Speed}, before: snapshot{bases: st.Plate.BeforeBases, outs: st.Plate.BeforeOuts}, pitches: st.Plate.Pitches}
		g.beginPlay(bl)
	} else if !st.Over {
		return nil, errors.New("engine: the state has no plate appearance in progress")
	}
	gm.g = g
	return gm, nil
}

// pitcherNamed finds a pitcher of the club by id: the starter or a reliever.
func (c *Club) pitcherNamed(id string) *Player {
	if c.Starter.ID == id {
		return &c.Starter
	}
	pen := &c.Bullpen
	switch id {
	case pen.Closer.ID:
		return &pen.Closer
	case pen.Setup.ID:
		return &pen.Setup
	}
	for i := range pen.Others {
		if pen.Others[i].ID == id {
			return &pen.Others[i]
		}
	}
	return nil
}
