package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
	"sync"
)

// Pitch is one pitch of a plate appearance: the count before it, its call,
// and in per-pitch play the beacon round it drew on.
type Pitch struct {
	Balls   int    `json:"balls"`
	Strikes int    `json:"strikes"`
	Call    string `json:"call"`
	Round   uint64 `json:"round,omitempty"`
}

// Pitch calls.
const (
	CallBall       = "ball"
	CallCalled     = "called strike"
	CallSwinging   = "swinging strike"
	CallFoul       = "foul"
	CallInPlay     = "in play"
	CallHitByPitch = "hit by pitch"
)

// Draws says how a game draws its randomness: from one stream of its seed
// for the whole game, or a stream for each pitch from the seed and the
// pitch's own beacon round.
type Draws string

// The draw modes.
const (
	DrawsGame  Draws = "game"
	DrawsPitch Draws = "pitch"
)

// The gap rule: how many beacon rounds, each 3 seconds apart, come between
// one pitch's round and the next's in per-pitch play.
const (
	GapPitch  = 5  // 15 seconds, with the bases empty
	GapRunner = 7  // 21 seconds, with a runner on
	GapBreak  = 40 // 2 minutes, after a half-inning ends or a pitcher changes mid-inning
)

// DerivationPitch names how a pitch's stream is derived.
const DerivationPitch = "sha256(seed|round|pitch)"

// pitchStream starts the stream for one pitch: the SHA-256 of the game's seed,
// the round's randomness, and the pitch's index seeds a PCG stream.
func pitchStream(seed Seed, randomness []byte, index int) *rand.Rand {
	buf := make([]byte, 24, 24+len(randomness))
	binary.BigEndian.PutUint64(buf[0:8], seed.A)
	binary.BigEndian.PutUint64(buf[8:16], seed.B)
	binary.BigEndian.PutUint64(buf[16:24], uint64(index))
	sum := sha256.Sum256(append(buf, randomness...))
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(sum[0:8]), binary.BigEndian.Uint64(sum[8:16])))
}

// The count shape: how the base rates a plate appearance's targets solve for
// pass through each count. Fouls come often with two strikes; a ball in play
// comes rarely on the first pitch and often with two strikes; with two strikes
// pitchers miss more and with three balls they throw strikes. The shape fixes
// the pitches a plate appearance takes, about 3.8 at league average; the
// solver fixes its outcomes.
var (
	foulByStrikes     = [3]float64{0.12, 0.15, 0.33}
	terminalByStrikes = [3]float64{0.48, 1.0, 1.35}
)

const (
	ballTwoStrikes    = 1.15
	ballThreeBalls    = 0.80
	strikeThreeBalls  = 1.30
	strikeTwoStrikes  = 0.90
	calledShare       = 0.6 // called strikes among strikes not fouled
	stealPitchesPerPA = 2.8 // the pitches a plate appearance offers a runner, so attempts per chance match the anchor
)

// chain is a plate appearance's per-pitch base rates: a terminal pitch, a
// ball in play or a hit by pitch; a ball; and a strike. Fouls come from the
// count shape alone.
type chain struct{ t, b, s float64 }

// at is the call distribution at a count: terminal, ball, strike, foul.
func (c chain) at(balls, strikes int) (t, b, s, f float64) {
	f = foulByStrikes[strikes]
	t = float64(c.t * terminalByStrikes[strikes])
	b, s = c.b, c.s
	if strikes == 2 {
		b, s = float64(b*ballTwoStrikes), float64(s*strikeTwoStrikes)
	}
	if balls == 3 {
		b, s = float64(b*ballThreeBalls), float64(s*strikeThreeBalls)
	}
	scale := float64((1 - f) / (t + b + s))
	return float64(t * scale), float64(b * scale), float64(s * scale), f
}

// absorb answers, from an empty count, the chance a plate appearance under
// the chain ends in a strikeout and in a walk, and the pitches it expects.
func (c chain) absorb() (k, bb, pitches float64) {
	var pk, pbb, ep [4][3]float64
	for balls := 3; balls >= 0; balls-- {
		for strikes := 2; strikes >= 0; strikes-- {
			t, b, s, f := c.at(balls, strikes)
			add := 1.0
			if strikes == 2 { // a foul keeps the count: fold the self-loop away
				t, b, s, add = float64(t/(1-f)), float64(b/(1-f)), float64(s/(1-f)), float64(1/(1-f))
				f = 0
			}
			var nk, nbb, nep float64
			if balls == 3 {
				nbb += b
			} else {
				nk += float64(b * pk[balls+1][strikes])
				nbb += float64(b * pbb[balls+1][strikes])
				nep += float64(b * ep[balls+1][strikes])
			}
			if strikes == 2 {
				nk += s
			} else {
				nk += float64(s * pk[balls][strikes+1])
				nbb += float64(s * pbb[balls][strikes+1])
				nep += float64(s * ep[balls][strikes+1])
				nk += float64(f * pk[balls][strikes+1])
				nbb += float64(f * pbb[balls][strikes+1])
				nep += float64(f * ep[balls][strikes+1])
			}
			_ = t
			pk[balls][strikes], pbb[balls][strikes], ep[balls][strikes] = nk, nbb, add+nep
		}
	}
	return pk[0][0], pbb[0][0], ep[0][0]
}

// chains keeps every chain solved so far by its targets, since a matchup's
// targets recur across games; the solve is a pure function of them, so a
// kept chain is the chain the solve would give.
var chains sync.Map

// solveChain finds the chain whose plate appearances end in a strikeout with
// chance k and a walk with chance bb, kept once solved: an outer bisection on the terminal rate
// for the two together, an inner one on the ball share for their ratio. Both
// move one way, so each bisection converges; 28 steps place each rate within
// a hundred-millionth.
func solveChain(k, bb float64) chain {
	key := [2]float64{k, bb}
	if c, ok := chains.Load(key); ok {
		return c.(chain)
	}
	lo, hi := 0.0, 1.0
	var c chain
	for range 28 {
		t := (lo + hi) / 2
		rlo, rhi := 0.0, 1.0
		for range 28 {
			r := (rlo + rhi) / 2
			c = chain{t: t, b: float64((1 - t) * r), s: float64((1 - t) * (1 - r))}
			gk, gbb, _ := c.absorb()
			if gk/(gk+gbb) > k/(k+bb) {
				rlo = r
			} else {
				rhi = r
			}
		}
		gk, gbb, _ := c.absorb()
		if gk+gbb > k+bb {
			lo = t
		} else {
			hi = t
		}
	}
	chains.Store(key, c)
	return c
}
