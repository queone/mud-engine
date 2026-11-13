package engine

import (
	"crypto/sha256"
	"encoding/binary"
)

// A draw is one random value in [0, 1) that its address alone fixes: the
// game's seed, the beacon randomness of the event's pitch, the event, the
// purpose, and the index. Asking for the same address again answers the same
// value, and asking for other draws earlier, later, or never changes
// nothing, so a new kind of draw never disturbs an existing one.

// DerivationDraw names how a draw is derived.
const DerivationDraw = "sha256(tag|seed|randomness|event|purpose|index)"

// drawTag starts every draw's hashed input.
const drawTag = "mud-engine draw v1"

// purpose names what a draw decides. An id keeps its meaning for the life of
// an engine model and is never reused; 0 is never used.
type purpose uint16

// The purposes and their indexes.
const (
	purposeCall       purpose = 1 // 0: the pitch's result
	purposeBatted     purpose = 2 // 0 homer, 1 hit, 2 hit type, 3 ground or air
	purposeRunner     purpose = 3 // the base the runner started on: 0 first, 1 second, 2 third
	purposeDoublePlay purpose = 4 // 0
	purposeSteal      purpose = 5 // 0 attempt, 1 success
	purposeDirection  purpose = 6 // 0
)

// The batted-ball and steal indexes.
const (
	battedHomer  = 0
	battedHit    = 1
	battedType   = 2
	battedGround = 3

	stealIndexAttempt = 0
	stealIndexSuccess = 1
)

// maxRandomness is the longest randomness a draw's 1-byte length can carry;
// a beacon round's is 32 bytes.
const maxRandomness = 255

// drawer addresses the draws of one event: a pitch, with a ball in play or a
// steal attempt on it.
type drawer struct {
	seed       Seed
	randomness []byte // the pitch's beacon randomness, or empty
	event      uint64 // the game's pitch ordinal from 0
}

// draw answers the value at the drawer's event for the purpose and index:
// SHA-256 of the tag, the seed's A and B as 8-byte big-endian integers, the
// randomness's length as 1 byte, the randomness, the event as an 8-byte
// big-endian integer, the purpose as a 2-byte one, and the index as a 4-byte
// one; the digest's first 8 bytes as a big-endian integer, shifted right by
// 11, times 2^-53.
func (d drawer) draw(p purpose, index uint32) float64 {
	return drawAt(d.seed, d.randomness, d.event, p, index)
}

func drawAt(seed Seed, randomness []byte, event uint64, p purpose, index uint32) float64 {
	buf := make([]byte, 0, len(drawTag)+8+8+1+len(randomness)+8+2+4)
	buf = append(buf, drawTag...)
	buf = binary.BigEndian.AppendUint64(buf, seed.A)
	buf = binary.BigEndian.AppendUint64(buf, seed.B)
	buf = append(buf, byte(len(randomness)))
	buf = append(buf, randomness...)
	buf = binary.BigEndian.AppendUint64(buf, event)
	buf = binary.BigEndian.AppendUint16(buf, uint16(p))
	buf = binary.BigEndian.AppendUint32(buf, index)
	sum := sha256.Sum256(buf)
	return float64(binary.BigEndian.Uint64(sum[:8])>>11) * 0x1p-53
}

// drawHook, nil in use, lets a test draw under a purpose of its own at the
// named points of a pitch, "before", "between", and "after", to show such
// draws disturb nothing; noDirection, false in use, lets a test leave the
// direction undrawn.
var (
	drawHook    func(d drawer, at string)
	noDirection bool
)

func hook(d drawer, at string) {
	if drawHook != nil {
		drawHook(d, at)
	}
}
