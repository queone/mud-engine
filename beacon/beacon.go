// Package beacon reads and verifies rounds of drand's quicknet chain, the
// public randomness beacon the League of Entropy runs: a signed random value
// every 3 seconds that fewer than a threshold of its members can neither
// predict nor bias. A round's moment is a pure function of its number, so a
// Day can be bound to a round before the round exists. Relays only carry
// rounds: every round is verified here against the chain's pinned public
// key, so a relay that serves a false round is skipped.
package beacon

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/sign/bdn"
)

// The quicknet chain: its hash, first round's moment, period, scheme, and
// public key, as https://api.drand.sh/<ChainHash>/info serves them.
const (
	ChainHash = "52db9ba70e0cc0f6eaf7803dd07447a1f5477735fd3f661792ba94600c84e971"
	Genesis   = 1692803367 // Unix seconds of round 1
	Period    = 3 * time.Second
	Scheme    = "bls-unchained-g1-rfc9380"
	PublicKey = "83cf0f2896adee7eb8b5f01fcad3912212c437e0073e911fb90022d3e760183c8c4b450b6a0a6c3ac6a5776a2d1064510d1fec758c921cc22b0e17e63aaf4bcb5ed66304de9cf809bd274ca73bab4af5a6e9c76a4bc09e76eae8991ef5ece45a"
)

// DefaultRelays are the public relays tried in order: three run by the drand
// team and one by Cloudflare. MUD_BEACON_RELAYS, a comma-separated list of
// base URLs, replaces them.
var DefaultRelays = []string{"https://api.drand.sh", "https://api2.drand.sh", "https://api3.drand.sh", "https://drand.cloudflare.com"}

// Round is one verified round: its number, its signature, and its
// randomness, the SHA-256 of the signature, both in hex.
type Round struct {
	Number     uint64 `json:"round"`
	Signature  string `json:"signature"`
	Randomness string `json:"randomness"`
}

// Bytes answers the round's randomness as bytes.
func (r Round) Bytes() []byte {
	b, _ := hex.DecodeString(r.Randomness)
	return b
}

// TimeOf is the moment a round is published.
func TimeOf(number uint64) time.Time {
	if number == 0 {
		number = 1
	}
	return time.Unix(Genesis, 0).Add(time.Duration(number-1) * Period).UTC()
}

// RoundAt is the first round published at or after t.
func RoundAt(t time.Time) uint64 {
	since := t.Sub(time.Unix(Genesis, 0))
	if since <= 0 {
		return 1
	}
	n := uint64(since / Period)
	if since%Period != 0 {
		n++
	}
	return n + 1
}

// NotYetError says a round's moment has not come, so no relay can serve it.
type NotYetError struct{ Number uint64 }

func (e NotYetError) Error() string {
	return fmt.Sprintf("beacon: round %d is published at %s", e.Number, TimeOf(e.Number).Format(time.RFC3339))
}

// Source serves and verifies rounds: the real network, or a stub in tests.
type Source interface {
	// Round fetches and verifies a round, or answers a NotYetError before
	// its moment.
	Round(ctx context.Context, number uint64) (Round, error)
	// Verify checks a stored round's signature and answers its randomness.
	Verify(number uint64, signature string) ([]byte, error)
}

// Verify checks a quicknet signature for the round against the pinned key
// and answers the round's randomness, the SHA-256 of the signature.
func Verify(number uint64, signature string) ([]byte, error) {
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("beacon: round %d signature is not hex: %w", number, err)
	}
	suite := bls12381.NewBLS12381Suite()
	key := suite.G2().Point()
	raw, _ := hex.DecodeString(PublicKey)
	if err := key.UnmarshalBinary(raw); err != nil {
		return nil, fmt.Errorf("beacon: the pinned key: %w", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], number)
	digest := sha256.Sum256(msg[:])
	// bdn's single-signature check is plain BLS; its changes to aggregation guard a
	// rogue-key attack that one fixed key cannot meet.
	if err := bdn.NewSchemeOnG1(suite).Verify(key, digest[:], sig); err != nil {
		return nil, fmt.Errorf("beacon: round %d signature does not verify against the quicknet key: %w", number, err)
	}
	sum := sha256.Sum256(sig)
	return sum[:], nil
}

// Client fetches rounds from public relays over HTTPS and verifies them.
type Client struct {
	Relays []string
	HTTP   *http.Client
	Now    func() time.Time
}

// New makes a client over MUD_BEACON_RELAYS when set, else DefaultRelays.
func New() *Client {
	relays := DefaultRelays
	if env := strings.TrimSpace(os.Getenv("MUD_BEACON_RELAYS")); env != "" {
		relays = nil
		for r := range strings.SplitSeq(env, ",") {
			if r = strings.TrimRight(strings.TrimSpace(r), "/"); r != "" {
				relays = append(relays, r)
			}
		}
	}
	return &Client{Relays: relays, HTTP: &http.Client{Timeout: 10 * time.Second}, Now: time.Now}
}

// Round fetches the round from the first relay that serves a round that
// verifies; before the round's moment it answers a NotYetError.
func (c *Client) Round(ctx context.Context, number uint64) (Round, error) {
	if TimeOf(number).After(c.Now()) {
		return Round{}, NotYetError{Number: number}
	}
	var failures []string
	for _, relay := range c.Relays {
		r, err := c.fetch(ctx, relay, number)
		if err == nil {
			return r, nil
		}
		failures = append(failures, err.Error())
	}
	return Round{}, fmt.Errorf("beacon: no relay served a valid round %d: %s; the Day waits for it", number, strings.Join(failures, "; "))
}

// Verify checks a stored round's signature against the pinned key.
func (c *Client) Verify(number uint64, signature string) ([]byte, error) {
	return Verify(number, signature)
}

func (c *Client) fetch(ctx context.Context, relay string, number uint64) (Round, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/public/%d", relay, ChainHash, number), nil)
	if err != nil {
		return Round{}, fmt.Errorf("%s: %w", relay, err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Round{}, fmt.Errorf("%s: %w", relay, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Round{}, fmt.Errorf("%s: status %d", relay, resp.StatusCode)
	}
	var body struct {
		Round     uint64 `json:"round"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Round{}, fmt.Errorf("%s: %w", relay, err)
	}
	if body.Round != number {
		return Round{}, fmt.Errorf("%s: served round %d for %d", relay, body.Round, number)
	}
	randomness, err := Verify(number, body.Signature)
	if err != nil {
		return Round{}, fmt.Errorf("%s: %w", relay, err)
	}
	return Round{Number: number, Signature: body.Signature, Randomness: hex.EncodeToString(randomness)}, nil
}

// Stub is a Source for tests: every round is available and its signature is
// the SHA-256 of "stub" and its number, which Verify checks in place of BLS.
type Stub struct {
	// Missing makes every round from it on unavailable, as during an outage.
	Missing uint64
	// Salt makes another stub chain, whose rounds differ from the plain one's.
	Salt string
}

// Round answers the stub's round, or an error from Missing on.
func (s Stub) Round(_ context.Context, number uint64) (Round, error) {
	if s.Missing != 0 && number >= s.Missing {
		return Round{}, errors.New("beacon: stub relays are down")
	}
	sig := stubSignature(s.Salt, number)
	sum := sha256.Sum256(sig)
	return Round{Number: number, Signature: hex.EncodeToString(sig), Randomness: hex.EncodeToString(sum[:])}, nil
}

// Verify checks a stub signature.
func (s Stub) Verify(number uint64, signature string) ([]byte, error) {
	sig := stubSignature(s.Salt, number)
	if signature != hex.EncodeToString(sig) {
		return nil, fmt.Errorf("beacon: round %d stub signature does not verify", number)
	}
	sum := sha256.Sum256(sig)
	return sum[:], nil
}

func stubSignature(salt string, number uint64) []byte {
	buf := append([]byte("stub"+salt), make([]byte, 8)...)
	binary.BigEndian.PutUint64(buf[len(buf)-8:], number)
	sum := sha256.Sum256(buf)
	return sum[:]
}
