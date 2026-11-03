package engine

import "testing"

// TestCalibrationLandsInsideLeagueBands plays two thousand games between
// league-average clubs and checks the rates against 2010 through 2024 MLB.
func TestCalibrationLandsInsideLeagueBands(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	rs := series(t, home, away, 7, 2000)
	var bat BatterLine
	var pit PitcherLine
	for _, r := range rs {
		bat.Add(r.Away.Batting)
		bat.Add(r.Home.Batting)
		pit.Add(r.Away.Pitching)
		pit.Add(r.Home.Pitching)
	}
	games := float64(len(rs))
	pa := float64(bat.PA)
	bands := []struct {
		name    string
		got     float64
		low, hi float64
	}{
		{"strikeout rate", float64(bat.K) / pa, 0.19, 0.24},
		{"walk rate", float64(bat.BB) / pa, 0.075, 0.095},
		{"hit by pitch rate", float64(bat.HBP) / pa, 0.008, 0.014},
		{"home run rate", float64(bat.HR) / pa, 0.025, 0.035},
		{"BABIP", float64(bat.Hits()-bat.HR) / float64(bat.AB-bat.K-bat.HR+bat.SF), 0.285, 0.305},
		{"runs per team per game", float64(bat.R) / (2 * games), 4.0, 4.9},
		{"steal attempts per team per game", float64(bat.SB+bat.CS) / (2 * games), 0.4, 1.0},
		{"steal success", float64(bat.SB) / float64(bat.SB+bat.CS), 0.65, 0.85},
		{"saves per game", float64(pit.SV) / games, 0.40, 0.65},
		{"quality starts per start", float64(pit.QS) / (2 * games), 0.35, 0.60},
	}
	for _, b := range bands {
		t.Logf("%s = %.4f (band %.3f to %.3f)", b.name, b.got, b.low, b.hi)
		if b.got < b.low || b.got > b.hi {
			t.Errorf("%s = %.4f, want %.3f to %.3f", b.name, b.got, b.low, b.hi)
		}
	}
}
