package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
)

// Seed is the pair of 64-bit values that starts a game's random stream.
type Seed struct {
	A          uint64 `json:"a"`
	B          uint64 `json:"b"`
	derivation string
}

// DerivationSHA names the derivation DeriveSeed performs.
const DerivationSHA = "sha256(world|season|game)"

// DeriveSeed derives a game seed from the world seed, season, and game id:
// the SHA-256 of their fixed 20-byte big-endian encoding, whose first
// sixteen bytes become the two seed values.
func DeriveSeed(worldSeed uint64, season uint32, gameID uint64) Seed {
	var buf [20]byte
	binary.BigEndian.PutUint64(buf[0:8], worldSeed)
	binary.BigEndian.PutUint32(buf[8:12], season)
	binary.BigEndian.PutUint64(buf[12:20], gameID)
	sum := sha256.Sum256(buf[:])
	return Seed{
		A:          binary.BigEndian.Uint64(sum[0:8]),
		B:          binary.BigEndian.Uint64(sum[8:16]),
		derivation: DerivationSHA,
	}
}

// DerivationBeacon names the derivation DeriveBeaconSeed performs.
const DerivationBeacon = "sha256(world|season|game|beacon)"

// DeriveBeaconSeed derives a game seed as DeriveSeed does with the Day's
// public beacon randomness appended, so no one can know the game until the
// beacon round exists.
func DeriveBeaconSeed(worldSeed uint64, season uint32, gameID uint64, randomness []byte) Seed {
	buf := make([]byte, 20, 20+len(randomness))
	binary.BigEndian.PutUint64(buf[0:8], worldSeed)
	binary.BigEndian.PutUint32(buf[8:12], season)
	binary.BigEndian.PutUint64(buf[12:20], gameID)
	sum := sha256.Sum256(append(buf, randomness...))
	return Seed{
		A:          binary.BigEndian.Uint64(sum[0:8]),
		B:          binary.BigEndian.Uint64(sum[8:16]),
		derivation: DerivationBeacon,
	}
}

// RawSeed wraps two values as a seed with no derivation, for tests and tools.
func RawSeed(a, b uint64) Seed { return Seed{A: a, B: b, derivation: "raw"} }

// Derivation reports how the seed was derived.
func (s Seed) Derivation() string {
	if s.derivation == "" {
		return "raw"
	}
	return s.derivation
}

// stream starts the deterministic random stream for the seed.
func (s Seed) stream() *rand.Rand { return rand.New(rand.NewPCG(s.A, s.B)) }
