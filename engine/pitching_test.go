package engine

import "testing"

func TestStartLimitFollowsStamina(t *testing.T) {
	for stamina, want := range map[int]int{1: 14, 50: 24, 99: 33} {
		if got := startLimit(&Pitching{Stamina: stamina}); got != want {
			t.Errorf("startLimit(%d) = %d, want %d", stamina, got, want)
		}
	}
}

func TestSaveSituationFollowsLeadAndRunners(t *testing.T) {
	cases := []struct {
		lead, runners int
		want          bool
	}{
		{1, 0, true}, {3, 0, true}, {4, 0, false}, {4, 2, true}, {5, 3, true}, {6, 3, false}, {0, 3, false}, {-1, 0, false},
	}
	for _, c := range cases {
		if got := saveSituation(c.lead, c.runners); got != c.want {
			t.Errorf("saveSituation(%d, %d) = %v, want %v", c.lead, c.runners, got, c.want)
		}
	}
}

func TestRelieverSelectionFollowsTheUsageRules(t *testing.T) {
	away, home := averageClub("Away", "a"), averageClub("Home", "h")
	g := newGame(&away, &home, RawSeed(1, 1))
	fld := g.clubs[1]
	pick := func(inning, homeLead int) string {
		g.inning = inning
		g.score = [2]int{0, 0}
		if homeLead > 0 {
			g.score[1] = homeLead
		} else {
			g.score[0] = -homeLead
		}
		p := g.pickReliever(fld)
		if p == nil {
			return ""
		}
		fld.used[p.ID] = true
		return p.ID
	}
	if got := pick(5, 0); got != "hr1" {
		t.Errorf("fifth inning tie picked %q, want hr1", got)
	}
	if got := pick(7, 2); got != "hsu" {
		t.Errorf("seventh with a two-run lead picked %q, want hsu", got)
	}
	if got := pick(8, 1); got != "hr2" {
		t.Errorf("eighth with the setup man used picked %q, want hr2", got)
	}
	if got := pick(9, 3); got != "hcl" {
		t.Errorf("ninth with a three-run lead picked %q, want hcl", got)
	}
	if got := pick(10, 4); got != "hr3" {
		t.Errorf("tenth with a four-run lead picked %q, want hr3", got)
	}
	if got := pick(11, 0); got != "" {
		t.Errorf("exhausted bullpen picked %q, want the current pitcher to stay", got)
	}
	g2 := newGame(&away, &home, RawSeed(1, 1))
	f2 := g2.clubs[1]
	g2.inning, g2.score = 9, [2]int{0, 5}
	if p := g2.pickReliever(f2); p.ID != "hr1" {
		t.Errorf("ninth with a five-run lead picked %q, want hr1", p.ID)
	}
	f2.nextOther = len(f2.club.Bullpen.Others)
	g2.score = [2]int{3, 0}
	if p := g2.pickReliever(f2); p.ID != "hcl" {
		t.Errorf("others used while trailing picked %q, want the unused closer", p.ID)
	}
}

func TestStarterLeavesAtTheLimitOrAfterFiveEarnedRuns(t *testing.T) {
	away, home := averageClub("Away", "a"), averageClub("Home", "h")
	g := newGame(&away, &home, RawSeed(1, 1))
	fld := g.clubs[1]
	g.inning = 3
	fld.cur.batters = startLimit(fld.club.Starter.Pitching)
	g.midHalf(fld)
	if fld.cur.pitcher.ID == "hsp" {
		t.Error("starter stayed past his batters-faced limit")
	}
	g = newGame(&away, &home, RawSeed(1, 1))
	fld = g.clubs[1]
	g.inning = 4
	fld.pitcher("hsp").ER = starterHookER
	g.startHalf(fld)
	if fld.cur.pitcher.ID == "hsp" {
		t.Error("starter stayed after five earned runs")
	}
	g = newGame(&away, &home, RawSeed(1, 1))
	fld = g.clubs[1]
	g.inning = 4
	fld.pitcher("hsp").ER = starterHookER - 1
	fld.cur.batters = startLimit(fld.club.Starter.Pitching) - 1
	g.startHalf(fld)
	g.midHalf(fld)
	if fld.cur.pitcher.ID != "hsp" {
		t.Error("starter left before his limit with four earned runs")
	}
}

func TestChangePitcherAwardsAHoldToASetupManWhoKeptTheLead(t *testing.T) {
	away, home := averageClub("Away", "a"), averageClub("Home", "h")
	g := newGame(&away, &home, RawSeed(1, 1))
	fld := g.clubs[1]
	g.inning, g.score = 8, [2]int{1, 3}
	g.changePitcher(fld) // starter out, setup in with a two-run lead
	if fld.cur.pitcher.ID != "hsu" || !fld.cur.saveSpot {
		t.Fatalf("expected the setup man in a save situation, got %q (save %v)", fld.cur.pitcher.ID, fld.cur.saveSpot)
	}
	fld.cur.outs = 3
	g.inning = 9
	g.changePitcher(fld) // closer in
	if fld.pitcher("hsu").HLD != 1 || fld.cur.pitcher.ID != "hcl" {
		t.Errorf("setup man HLD = %d, next pitcher %q", fld.pitcher("hsu").HLD, fld.cur.pitcher.ID)
	}
	if fld.pitcher("hsp").HLD != 0 {
		t.Error("starter was given a hold")
	}
}
