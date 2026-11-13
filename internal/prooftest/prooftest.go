// Package prooftest builds one game's proof, Day commitment, and box score
// for tests, on a real recorded quicknet round, shaped as a site serves them.
package prooftest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/queone/mud-engine/beacon"
	"github.com/queone/mud-engine/engine"
	"github.com/queone/mud-engine/proof"
	"github.com/queone/mud-engine/proof/notary"
)

// A real quicknet round, recorded from api.drand.sh.
const (
	Round      = 33620529
	Signature  = "8c86700b0b3ecd2c45aa44a19feeb796914a58a7e0d6fefaad6364520787162a22f89a9fe87f132c9e7dc10ff389e1d7"
	Randomness = "b174b768d659aad91d60b6cfc867e54ec7659ca0ce13c91c92359892f0e364ef"
)

// The game's place: game 7, baseball league 1, season 1, Day 3, slot 2.
const (
	GameID         = 7
	BaseballLeague = 1
	Season         = 1
	Day            = 3
	Slot           = 2
	Seed           = 42
)

// Game builds the game's proof, its Day's commitment, and its box score; the
// Day holds three games and this one is slot 2. Its pairings name the
// current model and whole-game draws, so the checks of a Day's commitment
// still replay.
func Game() (proof.Proof, proof.Commitment, proof.BoxScore, error) {
	var day []proof.Pairing
	for i := range 3 {
		day = append(day, proof.Pairing{Home: club(fmt.Sprintf("Home %d", i), fmt.Sprintf("h%d", i), 50+i), Away: club(fmt.Sprintf("Away %d", i), fmt.Sprintf("a%d", i), 50-i),
			Engine: engine.Version, Draws: string(engine.DrawsGame)})
	}
	inputs, err := json.Marshal(day)
	if err != nil {
		return proof.Proof{}, proof.Commitment{}, proof.BoxScore{}, err
	}
	hash, err := proof.HashInputs(inputs)
	if err != nil {
		return proof.Proof{}, proof.Commitment{}, proof.BoxScore{}, err
	}
	own, err := json.Marshal(day[Slot])
	if err != nil {
		return proof.Proof{}, proof.Commitment{}, proof.BoxScore{}, err
	}
	randomness, err := beacon.Verify(Round, Signature)
	if err != nil {
		return proof.Proof{}, proof.Commitment{}, proof.BoxScore{}, err
	}
	game := uint64(Day*16 + Slot)
	res, err := engine.Simulate(day[Slot].Home, day[Slot].Away, engine.DeriveBeaconSeed(Seed, Season, game, randomness), randomness)
	if err != nil {
		return proof.Proof{}, proof.Commitment{}, proof.BoxScore{}, err
	}
	committed := beacon.TimeOf(Round).Add(-24 * time.Hour).UTC()
	bl := proof.BaseballLeague{ID: BaseballLeague, Name: "MFB"}
	p := proof.Proof{GameID: GameID, BaseballLeague: bl, Season: Season, Day: Day, Slot: Slot, Game: game, BaseballLeagueSeed: Seed,
		Derivation: engine.DerivationBeacon, Chain: beacon.ChainHash, Scheme: beacon.Scheme, Round: Round, RoundAt: beacon.TimeOf(Round),
		Signature: Signature, Randomness: Randomness, InputsHash: hash, CommittedAt: committed, Inputs: own, Engine: engine.Version,
		Draws: string(engine.DrawsGame), Module: proof.Path + "@v0.1.0"}
	c := proof.Commitment{BaseballLeague: bl, Season: Season, Day: Day, Round: Round, RoundAt: beacon.TimeOf(Round), InputsHash: hash, CommittedAt: committed, Inputs: inputs}
	return p, c, Box(res, day[Slot]), nil
}

// Box is the box score a site serves for a result: lines by club, and runs by
// inning with a null for a home half not played.
func Box(res engine.Result, pair proof.Pairing) proof.BoxScore {
	box := proof.BoxScore{ID: GameID}
	for i, side := range []*proof.BoxSide{&box.Away, &box.Home} {
		c := []engine.Club{pair.Away, pair.Home}[i]
		side.Club = c.Name
		side.Runs = []int{res.Away.Runs, res.Home.Runs}[i]
		for f := range len(res.LineScore[0]) {
			if f < len(res.LineScore[i]) {
				side.Innings = append(side.Innings, &res.LineScore[i][f])
			} else {
				side.Innings = append(side.Innings, nil)
			}
		}
		prefix := strings.TrimSuffix(c.Lineup[0].Player.ID, "b1")
		for id, line := range res.Batters {
			if strings.HasPrefix(id, prefix) {
				side.Batting = append(side.Batting, proof.BoxBatter{EnginePlayer: str(id), Line: line})
				side.Hits += line.Hits()
			}
		}
		for id, line := range res.Pitchers {
			if strings.HasPrefix(id, prefix) {
				side.Pitching = append(side.Pitching, proof.BoxPitcher{EnginePlayer: str(id), Line: line})
			}
		}
	}
	return box
}

// str is an id as the JSON string a box score line carries.
func str(id string) json.RawMessage {
	raw, _ := json.Marshal(id)
	return raw
}

// Numbered is a box score as a site that numbers its players serves it: each
// line keeps its engine_player and carries a number in player, 1, 2, and so
// on in line order. The lines are copies, so the box score passed in stays as
// it was.
func Numbered(box proof.BoxScore) proof.BoxScore {
	n := 0
	next := func() json.RawMessage {
		n++
		return json.RawMessage(fmt.Sprint(n))
	}
	for _, side := range []*proof.BoxSide{&box.Away, &box.Home} {
		side.Batting = slices.Clone(side.Batting)
		for i := range side.Batting {
			side.Batting[i].Player = next()
		}
		side.Pitching = slices.Clone(side.Pitching)
		for i := range side.Pitching {
			side.Pitching[i].Player = next()
		}
	}
	return box
}

// Legacy is a box score in the shape saved before engine_player: each line's
// id in a string player and no engine_player. The lines are copies, so the
// box score passed in stays as it was.
func Legacy(box proof.BoxScore) proof.BoxScore {
	for _, side := range []*proof.BoxSide{&box.Away, &box.Home} {
		side.Batting = slices.Clone(side.Batting)
		for i := range side.Batting {
			side.Batting[i].Player, side.Batting[i].EnginePlayer = side.Batting[i].EnginePlayer, nil
		}
		side.Pitching = slices.Clone(side.Pitching)
		for i := range side.Pitching {
			side.Pitching[i].Player, side.Pitching[i].EnginePlayer = side.Pitching[i].EnginePlayer, nil
		}
	}
	return box
}

// club builds a club whose players carry one rating, ids starting prefix.
func club(name, prefix string, rating int) engine.Club {
	positions := [9]string{"C", "1B", "2B", "3B", "SS", "LF", "CF", "RF", "DH"}
	bat := engine.Batting{Contact: rating, Power: rating, Eye: rating, Speed: rating, Defense: rating}
	pit := engine.Pitching{Stuff: rating, Control: rating, Movement: rating, Stamina: rating}
	batter := func(id string) engine.Player {
		b := bat
		return engine.Player{ID: id, Name: id, Bats: engine.Right, Throws: engine.Right, Batting: &b}
	}
	pitcher := func(id string) engine.Player {
		p := pit
		return engine.Player{ID: id, Name: id, Bats: engine.Right, Throws: engine.Right, Pitching: &p}
	}
	c := engine.Club{Name: name, Starter: pitcher(prefix + "sp")}
	for i := range c.Lineup {
		c.Lineup[i] = engine.Slot{Player: batter(fmt.Sprintf("%sb%d", prefix, i+1)), Position: positions[i]}
	}
	c.Bullpen = engine.Bullpen{Closer: pitcher(prefix + "cl"), Setup: pitcher(prefix + "su"),
		Others: []engine.Player{pitcher(prefix + "r1"), pitcher(prefix + "r2"), pitcher(prefix + "r3")}}
	return c
}

// OwnGame is Game played under its own commitment instead of its Day's: the
// commitment covers slot 2 alone, its inputs that game's pairing, and its
// scheduled start is its round's publication, so it binds that round.
func OwnGame() (proof.Proof, proof.Commitment, proof.BoxScore, error) {
	p, c, box, err := Game()
	if err != nil {
		return p, c, box, err
	}
	hash, err := proof.HashInputs(p.Inputs)
	if err != nil {
		return p, c, box, err
	}
	p.CommitmentScope, p.InputsHash = proof.ScopeGame, hash
	c.Scope, c.Slot, c.Inputs, c.InputsHash, c.Start = proof.ScopeGame, Slot, p.Inputs, hash, beacon.TimeOf(Round)
	return p, c, box, nil
}

// AnchoredOwnGame is OwnGame with its commitment anchored a day before its
// round, and the notary whose Trust checks the anchor.
func AnchoredOwnGame() (proof.Proof, proof.Commitment, proof.BoxScore, *notary.Notary, error) {
	p, c, box, err := OwnGame()
	if err != nil {
		return p, c, box, nil, err
	}
	n, err := notary.New()
	if err != nil {
		return p, c, box, nil, err
	}
	a, err := n.Anchor(proof.GameStatement(Seed, Season, Day, Slot, c.Start, c.InputsHash), c.CommittedAt)
	if err != nil {
		return p, c, box, nil, err
	}
	c.Anchor = &a
	return p, c, box, n, nil
}

// AnchoredGame is Game with its Day's commitment anchored a day before its
// round, and the notary whose Trust checks the anchor.
func AnchoredGame() (proof.Proof, proof.Commitment, proof.BoxScore, *notary.Notary, error) {
	p, c, box, err := Game()
	if err != nil {
		return p, c, box, nil, err
	}
	n, err := notary.New()
	if err != nil {
		return p, c, box, nil, err
	}
	a, err := n.Anchor(proof.Statement(Seed, Season, Day, c.InputsHash), c.CommittedAt)
	if err != nil {
		return p, c, box, nil, err
	}
	c.Anchor = &a
	return p, c, box, n, nil
}

// WholeGame is a game under its own commitment whose pairing names the
// current engine model and whole-game draws.
func WholeGame() (proof.Proof, proof.Commitment, proof.BoxScore, error) {
	return ownCurrent(string(engine.DrawsGame))
}

// LiveGame is a game under its own commitment whose pairing names the
// current engine model and per-pitch draws: its first pitch draws on the bound round and
// each later one on a fake round FakeVerify accepts, and its proof lists
// every pitch's round and signature.
func LiveGame() (proof.Proof, proof.Commitment, proof.BoxScore, error) {
	return ownCurrent(string(engine.DrawsPitch))
}

func ownCurrent(draws string) (proof.Proof, proof.Commitment, proof.BoxScore, error) {
	p, c, _, err := OwnGame()
	if err != nil {
		return p, c, proof.BoxScore{}, err
	}
	pair := proof.Pairing{Home: club("Home 2", "h2", 52), Away: club("Away 2", "a2", 48), Engine: engine.Version, Draws: draws}
	inputs, err := json.Marshal(pair)
	if err != nil {
		return p, c, proof.BoxScore{}, err
	}
	hash, err := proof.HashInputs(inputs)
	if err != nil {
		return p, c, proof.BoxScore{}, err
	}
	randomness, err := beacon.Verify(Round, Signature)
	if err != nil {
		return p, c, proof.BoxScore{}, err
	}
	seed := engine.DeriveBeaconSeed(Seed, Season, uint64(Day*16+Slot), randomness)
	var res engine.Result
	if draws == string(engine.DrawsGame) {
		res, err = engine.Simulate(pair.Home, pair.Away, seed, randomness)
	} else {
		res, err = engine.SimulateLive(pair.Home, pair.Away, seed, Round, func(round uint64) ([]byte, error) { return FakeVerify(round, SignatureOf(round)) })
	}
	if err != nil {
		return p, c, proof.BoxScore{}, err
	}
	p.Inputs, p.InputsHash, p.Engine, p.Draws = inputs, hash, engine.Version, draws
	c.Inputs, c.InputsHash = inputs, hash
	if draws == string(engine.DrawsPitch) {
		for _, play := range res.Plays {
			for _, pt := range play.Pitches {
				p.Rounds = append(p.Rounds, proof.PitchRound{Round: pt.Round, Signature: SignatureOf(pt.Round)})
			}
		}
	}
	return p, c, Box(res, pair), nil
}

// WithModel rewrites the committed pairing's engine model in the commitment
// and the proof, hashes included, so only the engine link can object.
func WithModel(p *proof.Proof, c *proof.Commitment, model string) error {
	var pair proof.Pairing
	if err := json.Unmarshal(c.Inputs, &pair); err != nil {
		return err
	}
	pair.Engine = model
	inputs, err := json.Marshal(pair)
	if err != nil {
		return err
	}
	hash, err := proof.HashInputs(inputs)
	if err != nil {
		return err
	}
	p.Inputs, p.InputsHash, p.Engine = inputs, hash, model
	c.Inputs, c.InputsHash = inputs, hash
	return nil
}

// SignatureOf is the signature FakeVerify accepts for a round: the pinned
// real one for the pinned round, a fake one for any other.
func SignatureOf(round uint64) string {
	if round == Round {
		return Signature
	}
	return FakeSignature(round)
}

// FakeSignature is the signature FakeVerify accepts for a round other than
// the real one the fixtures pin.
func FakeSignature(round uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], round)
	sum := sha256.Sum256(append([]byte("signature"), buf[:]...))
	return hex.EncodeToString(sum[:])
}

// FakeVerify verifies the real pinned round as the beacon does and any other
// round by its fake signature, answering randomness of the round's own.
func FakeVerify(round uint64, signature string) ([]byte, error) {
	if round == Round {
		return beacon.Verify(round, signature)
	}
	if signature != FakeSignature(round) {
		return nil, fmt.Errorf("round %d's signature does not verify", round)
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], round)
	sum := sha256.Sum256(append([]byte("randomness"), buf[:]...))
	return sum[:], nil
}
