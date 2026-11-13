package engine

import (
	"crypto/sha256"
	"encoding/json"
	"testing"
)

// The address alone fixes a draw: vectors computed apart from this code pin
// the encoding and the value, the same address answers the same value, and
// changing any one field changes it.
func TestADrawIsFixedByItsAddress(t *testing.T) {
	round := sha256.Sum256([]byte("round"))
	seed := RawSeed(0x0123456789abcdef, 0xfedcba9876543210)
	for _, v := range []struct {
		seed       Seed
		randomness []byte
		event      uint64
		p          purpose
		index      uint32
		want       float64
	}{
		{RawSeed(1, 2), nil, 0, purposeCall, 0, 0x1.ca4e84fab9d58p-2},
		{seed, round[:], 42, purposeDirection, 0, 0x1.ddf9402183800p-9},
		{seed, round[:], 42, purposeRunner, 2, 0x1.4dbff579be300p-8},
	} {
		d := drawer{seed: v.seed, randomness: v.randomness, event: v.event}
		if got := d.draw(v.p, v.index); got != v.want {
			t.Errorf("draw at event %d, purpose %d, index %d = %x, want %x", v.event, v.p, v.index, got, v.want)
		}
		if again := d.draw(v.p, v.index); again != v.want {
			t.Errorf("the same address answered %x, then %x", v.want, again)
		}
	}
	base := drawer{seed: seed, randomness: round[:], event: 42}
	want := base.draw(purposeRunner, 2)
	other := sha256.Sum256([]byte("other round"))
	for name, got := range map[string]float64{
		"seed A":     drawer{seed: RawSeed(seed.A+1, seed.B), randomness: round[:], event: 42}.draw(purposeRunner, 2),
		"seed B":     drawer{seed: RawSeed(seed.A, seed.B+1), randomness: round[:], event: 42}.draw(purposeRunner, 2),
		"randomness": drawer{seed: seed, randomness: other[:], event: 42}.draw(purposeRunner, 2),
		"no beacon":  drawer{seed: seed, event: 42}.draw(purposeRunner, 2),
		"event":      drawer{seed: seed, randomness: round[:], event: 43}.draw(purposeRunner, 2),
		"purpose":    base.draw(purposeDirection, 2),
		"index":      base.draw(purposeRunner, 1),
	} {
		if got == want {
			t.Errorf("changing the %s left the draw at %x", name, want)
		}
	}
	for i := range uint32(1000) {
		if u := base.draw(purposeCall, i); u < 0 || u >= 1 {
			t.Fatalf("draw %d is %v, outside [0, 1)", i, u)
		}
	}
}

// testPurpose is a purpose of the test's own, as a future measurement such
// as exit velocity would be.
const testPurpose purpose = 0xffff

// playEvery plays a game whole, a pitch at a time, and resumed from its JSON
// state after every pitch, in both draw modes, and answers each result by
// name.
func playEvery(t *testing.T, home, away Club, seed Seed) map[string]Result {
	t.Helper()
	bound := fakeRandomness(4000)
	out := map[string]Result{}
	whole, err := Simulate(home, away, seed, bound)
	if err != nil {
		t.Fatal(err)
	}
	out["whole"] = whole
	if out["live"], err = SimulateLive(home, away, seed, 9000, fakeRounds); err != nil {
		t.Fatal(err)
	}
	for _, draws := range []Draws{DrawsGame, DrawsPitch} {
		gm, err := Start(home, away, seed, draws, 9000, bound)
		if err != nil {
			t.Fatal(err)
		}
		for !gm.Done() {
			raw, err := json.Marshal(gm)
			if err != nil {
				t.Fatal(err)
			}
			var st State
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}
			if gm, err = Resume(st, home, away); err != nil {
				t.Fatal(err)
			}
			round, _ := gm.Next()
			if _, err := gm.Pitch(fakeRandomness(round)); err != nil {
				t.Fatal(err)
			}
		}
		out["resumed "+string(draws)] = gm.Result()
	}
	if mustJSON(t, out["resumed game"]) != mustJSON(t, out["whole"]) || mustJSON(t, out["resumed pitch"]) != mustJSON(t, out["live"]) {
		t.Fatal("a game resumed after every pitch differs from the game played straight through")
	}
	return out
}

// A purpose of its own disturbs nothing: draws under it before, between, and
// after the existing draws leave every structured fact identical, direction
// included; with the direction draw disabled, every fact but direction stays
// and no play has one. Each case runs whole, a pitch at a time, and resumed
// after every pitch, in both draw modes.
func TestANewPurposeDisturbsNothing(t *testing.T) {
	t.Cleanup(func() { drawHook, noDirection = nil, false })
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	for i := range uint64(20) {
		seed := DeriveSeed(91, 1, i)
		base := playEvery(t, home, away, seed)

		extra, seen := 0, map[float64]bool{}
		drawHook = func(d drawer, at string) {
			for j := range uint32(3) {
				seen[d.draw(testPurpose, j)] = true
				extra++
			}
		}
		withExtra := playEvery(t, home, away, seed)
		drawHook = nil
		if extra == 0 || len(seen) < 100 {
			t.Fatalf("the test purpose drew %d values, only %d distinct", extra, len(seen))
		}

		noDirection = true
		without := playEvery(t, home, away, seed)
		noDirection = false

		for name, want := range base {
			if mustJSON(t, withExtra[name]) != mustJSON(t, want) {
				t.Fatalf("seed %d, %s: draws under a new purpose changed the game", i, name)
			}
			stripped := want
			stripped.Plays = nil
			directions := 0
			for _, p := range want.Plays {
				if p.Toward != "" {
					directions++
				}
				p.Toward = ""
				stripped.Plays = append(stripped.Plays, p)
			}
			if directions == 0 {
				t.Fatalf("seed %d, %s: no play has a direction", i, name)
			}
			for _, p := range without[name].Plays {
				if p.Toward != "" {
					t.Fatalf("seed %d, %s: a play has direction %s with the direction draw disabled", i, name, p.Toward)
				}
			}
			if mustJSON(t, without[name]) != mustJSON(t, stripped) {
				t.Fatalf("seed %d, %s: leaving the direction undrawn changed the game", i, name)
			}
		}
	}
}
