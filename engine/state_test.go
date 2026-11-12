package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

// fakeRandomness stands in for a beacon round's randomness in tests.
func fakeRandomness(round uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], round)
	sum := sha256.Sum256(buf[:])
	return sum[:]
}

func fakeRounds(round uint64) ([]byte, error) { return fakeRandomness(round), nil }

// mustJSON is a result's JSON, so two results compare whole.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A game played whole and a game played a pitch at a time give one result,
// in both draw modes; per-pitch play records each pitch's round, game play
// none, and the result names its draws.
func TestAGameReplaysWholeAndPitchByPitch(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	seed := DeriveSeed(7, 1, 17)
	whole := mustSimulate(t, home, away, seed)
	gm, err := Start(home, away, seed, DrawsGame, 0)
	if err != nil {
		t.Fatal(err)
	}
	for !gm.Done() {
		if round, ok := gm.Next(); round != 0 || !ok {
			t.Fatalf("game draws named round %d, ok %v", round, ok)
		}
		if _, err := gm.Pitch(nil); err != nil {
			t.Fatal(err)
		}
	}
	if mustJSON(t, gm.Result()) != mustJSON(t, whole) || whole.Draws != DrawsGame {
		t.Fatal("a game played pitch by pitch differs from the same game played whole")
	}
	if _, err := gm.Pitch(nil); err == nil {
		t.Error("a finished game threw another pitch")
	}
	for _, p := range whole.Plays {
		for _, pt := range p.Pitches {
			if pt.Round != 0 {
				t.Fatal("game draws recorded a round on a pitch")
			}
		}
	}

	live, err := SimulateLive(home, away, seed, 5000, fakeRounds)
	if err != nil {
		t.Fatal(err)
	}
	gm, err = Start(home, away, seed, DrawsPitch, 5000)
	if err != nil {
		t.Fatal(err)
	}
	for !gm.Done() {
		round, _ := gm.Next()
		if _, err := gm.Pitch(fakeRandomness(round)); err != nil {
			t.Fatal(err)
		}
	}
	if mustJSON(t, gm.Result()) != mustJSON(t, live) || live.Draws != DrawsPitch || live.Derivation != seed.Derivation() {
		t.Fatal("a live game played pitch by pitch differs from SimulateLive")
	}
	if mustJSON(t, live.Plays) == mustJSON(t, whole.Plays) {
		t.Error("per-pitch draws played the same game as the whole-game stream")
	}
	pitches := 0
	for _, p := range live.Plays {
		for _, pt := range p.Pitches {
			pitches++
			if pt.Round == 0 {
				t.Fatal("per-pitch draws left a pitch without its round")
			}
		}
	}
	if pitches != gm.Pitches() || live.Plays[0].Pitches[0].Round != 5000 {
		t.Errorf("%d pitches logged of %d thrown; the first on round %d", pitches, gm.Pitches(), live.Plays[0].Pitches[0].Round)
	}
}

// A game saved as JSON after every pitch and resumed with the committed
// clubs, the game it came from discarded, plays on to the whole game's
// result, in both draw modes, through a half-inning a caught stealing ended
// mid-plate-appearance; a state from another model is refused.
func TestAGameResumesFromItsStateAfterEveryPitch(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	// A seed whose game voids a plate appearance, so the resume crosses one.
	var seed Seed
	for i := range uint64(400) {
		s := DeriveSeed(41, 1, i)
		for _, p := range mustSimulate(t, home, away, s).Plays {
			if p.Outcome == "" {
				seed = s
			}
		}
		if seed.A != 0 {
			break
		}
	}
	if seed.A == 0 {
		t.Fatal("no game among 400 voided a plate appearance")
	}
	for _, draws := range []Draws{DrawsGame, DrawsPitch} {
		var want Result
		if draws == DrawsGame {
			want = mustSimulate(t, home, away, seed)
		} else {
			r, err := SimulateLive(home, away, seed, 9000, fakeRounds)
			if err != nil {
				t.Fatal(err)
			}
			want = r
		}
		gm, err := Start(home, away, seed, draws, 9000)
		if err != nil {
			t.Fatal(err)
		}
		saves := 0
		for !gm.Done() {
			raw, err := json.Marshal(gm)
			if err != nil {
				t.Fatal(err)
			}
			gm = nil
			var st State
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}
			if gm, err = Resume(st, home, away); err != nil {
				t.Fatalf("%s, after %d pitches: %v", draws, st.Pitches, err)
			}
			saves++
			round, _ := gm.Next()
			if _, err := gm.Pitch(fakeRandomness(round)); err != nil {
				t.Fatal(err)
			}
		}
		if got := gm.Result(); mustJSON(t, got) != mustJSON(t, want) {
			t.Fatalf("%s: the game resumed after each of %d pitches differs from the game played whole", draws, saves)
		}
		if saves != gm.Pitches() {
			t.Errorf("%s: %d saves for %d pitches", draws, saves, gm.Pitches())
		}
		raw, _ := json.Marshal(gm)
		var st State
		_ = json.Unmarshal(raw, &st)
		if !st.Over || st.Plate != nil || st.Stream != nil {
			t.Errorf("%s: the finished state is over %v with a plate appearance %v and a stream of %d bytes", draws, st.Over, st.Plate != nil, len(st.Stream))
		}
	}
	gm, _ := Start(home, away, seed, DrawsGame, 0)
	st := gm.State()
	st.Version = "2"
	if _, err := Resume(st, home, away); err == nil {
		t.Error("a model 2 state resumed on model 3")
	}
	gm, _ = Start(home, away, seed, DrawsPitch, 1)
	if _, err := gm.Pitch(nil); err == nil {
		t.Error("a per-pitch game threw without its round's randomness")
	}
	if _, err := Start(home, away, seed, Draws("inning"), 0); err == nil {
		t.Error("an unknown draw mode started a game")
	}
	if _, err := SimulateLive(home, away, seed, 1, func(uint64) ([]byte, error) { return nil, errors.New("no relay") }); err == nil {
		t.Error("a live replay went on without its round")
	}
}

// Each pitch draws on its own round: a change to one round's randomness
// leaves every earlier pitch as it was and changes that pitch or a later one.
func TestEachPitchDrawsOnItsOwnRound(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	seed := DeriveSeed(7, 1, 23)
	base, err := SimulateLive(home, away, seed, 100, fakeRounds)
	if err != nil {
		t.Fatal(err)
	}
	var pitches []Pitch
	for _, p := range base.Plays {
		pitches = append(pitches, p.Pitches...)
	}
	for _, n := range []int{0, 1, 40, 150} {
		target := pitches[n].Round
		moved, err := SimulateLive(home, away, seed, 100, func(round uint64) ([]byte, error) {
			if round == target {
				return fakeRandomness(round + 1_000_000), nil
			}
			return fakeRandomness(round), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var got []Pitch
		for _, p := range moved.Plays {
			got = append(got, p.Pitches...)
		}
		for i := range n {
			if got[i] != pitches[i] {
				t.Fatalf("changing pitch %d's round changed pitch %d", n, i)
			}
		}
		if mustJSON(t, got) == mustJSON(t, pitches) {
			t.Errorf("changing pitch %d's round changed nothing", n)
		}
	}
}

// The gap rule is the engine's: the next pitch's round is the last one's
// plus 5 with the bases empty, 7 with a runner on, and 40 after a
// half-inning ends or a pitcher changes mid-inning; the first pitch's round
// is the one Start was given.
func TestTheGapRuleFollowsTheState(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	gm, err := Start(home, away, DeriveSeed(7, 1, 29), DrawsPitch, 777)
	if err != nil {
		t.Fatal(err)
	}
	if round, _ := gm.Next(); round != 777 {
		t.Fatalf("the first pitch draws on round %d, not 777", round)
	}
	seen := map[uint64]bool{}
	for !gm.Done() {
		g := gm.g
		round, _ := gm.Next()
		inning, top, pitcher := g.inning, g.top, g.fielding().cur.pitcher.ID
		if _, err := gm.Pitch(fakeRandomness(round)); err != nil {
			t.Fatal(err)
		}
		if gm.Done() {
			break
		}
		next, _ := gm.Next()
		want := uint64(GapPitch)
		switch {
		case g.inning != inning || g.top != top || g.fielding().cur.pitcher.ID != pitcher:
			want = GapBreak
		case g.runnersOn() > 0:
			want = GapRunner
		}
		if next-round != want {
			t.Fatalf("after pitch %d the gap is %d, want %d", gm.Pitches(), next-round, want)
		}
		seen[want] = true
	}
	if !seen[GapPitch] || !seen[GapRunner] || !seen[GapBreak] {
		t.Errorf("the game used gaps %v; want all three", seen)
	}
}
