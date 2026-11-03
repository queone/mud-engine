package engine

// Pitcher usage and decision thresholds.
const (
	starterHookER  = 5  // earned runs that end the starter's outing after a half-inning
	starterWinOuts = 15 // outs a starter needs to be the winning pitcher
	relieverOuts   = 3  // outs after which a reliever is replaced at the next half-inning
	saveLeadMax    = 3  // largest lead for a closer, setup, or one-inning save situation
	saveLongOuts   = 9  // outs that earn a save regardless of the lead at entry
	qualityOuts    = 18 // outs a quality start needs
	qualityER      = 3  // earned runs a quality start allows
)

// startLimit is the starter's batters-faced limit from stamina: 14 at 1, 24 at 50, 33 at 99.
func startLimit(p *Pitching) int { return 14 + p.Stamina/5 }

// stint tracks the current pitcher's outing for decisions.
type stint struct {
	pitcher      *Player
	outs         int
	batters      int
	entryLead    int
	entryRunners int
	saveSpot     bool // entered in a save situation
	blown        bool
}

// saveSituation reports whether a pitcher entering now with this lead and
// these runners on base is in a save situation.
func saveSituation(lead, runners int) bool {
	return lead > 0 && (lead <= saveLeadMax || lead <= runners+2)
}

// startHalf replaces the fielding team's pitcher at the start of a half-inning
// when the starter has allowed enough earned runs or a reliever has finished
// his inning.
func (g *game) startHalf(fld *team) {
	s := &fld.cur
	switch {
	case s.pitcher.ID == fld.club.Starter.ID:
		if fld.pitcher(s.pitcher.ID).ER >= starterHookER {
			g.changePitcher(fld)
		}
	case s.outs >= relieverOuts:
		g.changePitcher(fld)
	}
}

// midHalf replaces the starter immediately when he reaches his batters-faced limit.
func (g *game) midHalf(fld *team) {
	s := &fld.cur
	if s.pitcher.ID == fld.club.Starter.ID && s.batters >= startLimit(s.pitcher.Pitching) {
		g.changePitcher(fld)
	}
}

// changePitcher brings in the reliever the situation calls for, if any
// remain, and settles the outgoing pitcher's hold.
func (g *game) changePitcher(fld *team) {
	next := g.pickReliever(fld)
	if next == nil {
		return
	}
	lead := g.lead(fld)
	s := &fld.cur
	if s.saveSpot && s.outs >= 1 && lead > 0 && s.pitcher.ID != fld.club.Starter.ID {
		fld.pitcher(s.pitcher.ID).HLD++
	}
	fld.cur = stint{
		pitcher:      next,
		entryLead:    lead,
		entryRunners: g.runnersOn(),
	}
	fld.cur.saveSpot = saveSituation(lead, fld.cur.entryRunners)
	fld.used[next.ID] = true
	fld.order = append(fld.order, next.ID)
}

// pickReliever applies the usage rules: closer in the ninth or later with a
// small lead, setup man in the seventh or eighth with a small lead, then the
// other relievers one inning each in order, then any unused closer or setup
// man. It returns nil when the bullpen is exhausted, so the last reliever stays.
func (g *game) pickReliever(fld *team) *Player {
	lead := g.lead(fld)
	small := lead >= 1 && lead <= saveLeadMax
	pen := &fld.club.Bullpen
	if g.inning >= 9 && small && !fld.used[pen.Closer.ID] {
		return &pen.Closer
	}
	if (g.inning == 7 || g.inning == 8) && small && !fld.used[pen.Setup.ID] {
		return &pen.Setup
	}
	if fld.nextOther < len(pen.Others) {
		p := &pen.Others[fld.nextOther]
		fld.nextOther++
		return p
	}
	if !fld.used[pen.Closer.ID] {
		return &pen.Closer
	}
	if !fld.used[pen.Setup.ID] {
		return &pen.Setup
	}
	return nil
}

// decide awards the win, loss, save, and quality starts once the game is over.
func (g *game) decide() {
	w, l := g.clubs[1], g.clubs[0]
	if g.score[0] > g.score[1] {
		w, l = l, w
	}
	winner := w.leadPitcher
	if winner == w.club.Starter.ID && w.pitcher(winner).Outs < starterWinOuts && len(w.order) > 1 {
		winner = w.order[1]
	}
	w.pitcher(winner).W++
	l.pitcher(l.losePitcher).L++

	s := &w.cur
	fin := s.pitcher.ID
	if fin != winner && fin != w.club.Starter.ID && s.entryLead > 0 && s.outs >= 1 {
		if (s.entryLead <= saveLeadMax && s.outs >= relieverOuts) || s.entryLead <= s.entryRunners+2 || s.outs >= saveLongOuts {
			w.pitcher(fin).SV++
		}
	}
	for _, t := range g.clubs {
		sp := t.pitcher(t.club.Starter.ID)
		if sp.Outs >= qualityOuts && sp.ER <= qualityER {
			sp.QS++
		}
	}
}
