package engine

import (
	"encoding/json"
	"testing"
)

// The benchmarks play one fixed pairing a game per iteration: whole, a pitch
// at a time, and resumed from its JSON state after every pitch, each with a
// cold chain-solver cache, emptied before every game, and a warm one.

func benchClubs() (Club, Club) {
	home := ratedClub("Mudville", "m", Batting{60, 55, 45, 70, 50}, Pitching{55, 45, 60, 65})
	away := ratedClub("Visitors", "v", Batting{45, 65, 60, 40, 55}, Pitching{60, 60, 40, 50})
	return home, away
}

// benchSeed is game i's seed, so every iteration plays a different game of
// the same fixed sequence.
func benchSeed(i int) Seed { return DeriveSeed(2026, 1, uint64(i)) }

func benchCaches(b *testing.B, run func(b *testing.B)) {
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			chains.Clear()
			run(b)
		}
	})
	b.Run("warm", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			run(b)
		}
	})
}

func BenchmarkWholeGame(b *testing.B) {
	home, away := benchClubs()
	i := 0
	benchCaches(b, func(b *testing.B) {
		i++
		if _, err := benchWhole(home, away, benchSeed(i)); err != nil {
			b.Fatal(err)
		}
	})
}

func BenchmarkLiveGame(b *testing.B) {
	home, away := benchClubs()
	i := 0
	benchCaches(b, func(b *testing.B) {
		i++
		gm, err := benchStart(home, away, benchSeed(i))
		if err != nil {
			b.Fatal(err)
		}
		for !gm.Done() {
			round, _ := gm.Next()
			if _, err := gm.Pitch(fakeRandomness(round)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkResumedGame(b *testing.B) {
	home, away := benchClubs()
	i := 0
	benchCaches(b, func(b *testing.B) {
		i++
		gm, err := benchStart(home, away, benchSeed(i))
		if err != nil {
			b.Fatal(err)
		}
		for !gm.Done() {
			round, _ := gm.Next()
			if _, err := gm.Pitch(fakeRandomness(round)); err != nil {
				b.Fatal(err)
			}
			raw, err := json.Marshal(gm)
			if err != nil {
				b.Fatal(err)
			}
			var st State
			if err := json.Unmarshal(raw, &st); err != nil {
				b.Fatal(err)
			}
			if gm, err = Resume(st, home, away); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// benchWhole plays a game whole under the model in the code.
func benchWhole(home, away Club, seed Seed) (Result, error) {
	return Simulate(home, away, seed, fakeRandomness(4000))
}

// benchStart starts a game a pitch at a time under the model in the code.
func benchStart(home, away Club, seed Seed) (*Game, error) {
	return Start(home, away, seed, DrawsPitch, 9000, nil)
}
