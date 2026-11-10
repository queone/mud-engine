// Package proof checks a game against its proof. A game's result follows
// from its baseball league's public seed, its season and game number, a drand beacon
// round, and its inputs, the two clubs as the engine received them; the Day's
// commitment is the hash of every game's inputs, stored before the Day's
// round existed. Check replays the game from those and compares every link
// with what the site serves.
package proof

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/queone/mud-engine/beacon"
	"github.com/queone/mud-engine/engine"
)

// Path is this module's import path.
const Path = "github.com/queone/mud-engine"

// Pairing is one game's two clubs as the engine received them: lineups,
// rotation, bullpen, ratings, fatigue, and injuries at first pitch. A Day's
// inputs are its pairings by slot.
type Pairing struct {
	Home engine.Club `json:"home"`
	Away engine.Club `json:"away"`
}

// HashInputs is the hex SHA-256 of a Day's inputs in compact JSON, the form
// they are stored in. Indented or compact, the same inputs give one hash.
func HashInputs(inputs []byte) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, inputs); err != nil {
		return "", fmt.Errorf("proof: the inputs are not JSON: %w", err)
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// BaseballLeague names the baseball league a game belongs to.
type BaseballLeague struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Proof is everything one game's result follows from, as the site serves it
// at /api/games/{id}/proof.
type Proof struct {
	GameID             int64           `json:"game_id"`
	BaseballLeague     BaseballLeague  `json:"baseball_league"`
	Season             uint32          `json:"season"`
	Day                int             `json:"day"`
	Slot               int             `json:"slot"`
	Game               uint64          `json:"game"` // Day*16+slot, the seed's game number
	BaseballLeagueSeed uint64          `json:"baseball_league_seed"`
	Derivation         string          `json:"derivation"`
	Chain              string          `json:"chain"`
	Scheme             string          `json:"scheme"`
	Round              uint64          `json:"round"`
	RoundAt            time.Time       `json:"round_at"`
	Signature          string          `json:"signature"`
	Randomness         string          `json:"randomness"`
	InputsHash         string          `json:"inputs_hash"`
	CommittedAt        time.Time       `json:"committed_at"`
	Inputs             json.RawMessage `json:"inputs"`
	Engine             string          `json:"engine_version"`
	// Module is the engine module and version the site ran, as PATH@VERSION.
	Module string `json:"module"`
}

// Commitment is a Day's commitment as the site serves it at
// /api/baseball-leagues/{id}/seasons/{n}/days/{d}/commitment: the beacon round bound
// to the Day, the hash of the Day's inputs, when it was stored, and the
// inputs themselves, so anyone can recompute the hash.
type Commitment struct {
	BaseballLeague BaseballLeague  `json:"baseball_league"`
	Season         uint32          `json:"season"`
	Day            int             `json:"day"`
	Round          uint64          `json:"round"`
	RoundAt        time.Time       `json:"round_at"`
	InputsHash     string          `json:"inputs_hash"`
	CommittedAt    time.Time       `json:"committed_at"`
	Inputs         json.RawMessage `json:"inputs"`
	Anchor         *Anchor         `json:"anchor"` // nil for a Day committed without one
}

// BoxScore is the part of a served box score, /api/games/{id}, that a replay
// must reproduce.
type BoxScore struct {
	ID   int64   `json:"id"`
	Away BoxSide `json:"away"`
	Home BoxSide `json:"home"`
}

// BoxSide is one club's half of a box score: runs, hits, runs by inning, a
// null for a bottom half the home side did not need, and every batter's and
// pitcher's line.
type BoxSide struct {
	Club     string       `json:"club"`
	Runs     int          `json:"runs"`
	Hits     int          `json:"hits"`
	Innings  []*int       `json:"innings"`
	Batting  []BoxBatter  `json:"batting"`
	Pitching []BoxPitcher `json:"pitching"`
}

// BoxBatter is one batter's line in a box score. EnginePlayer, the JSON field
// engine_player, is the batter's id in the game's inputs, which the replay's
// lines carry. Player, the JSON field player, matters only in a box score
// saved before engine_player existed, which carries that id there as a
// string; a newer box score may carry any other value there, such as a
// number. Neither field's JSON type is assumed when reading, so an unexpected
// one fails its line's replay check instead of the whole box score.
type BoxBatter struct {
	EnginePlayer json.RawMessage   `json:"engine_player,omitempty"`
	Player       json.RawMessage   `json:"player,omitempty"`
	Line         engine.BatterLine `json:"line"`
}

// BoxPitcher is one pitcher's line in a box score; EnginePlayer and Player
// are as a batter's.
type BoxPitcher struct {
	EnginePlayer json.RawMessage    `json:"engine_player,omitempty"`
	Player       json.RawMessage    `json:"player,omitempty"`
	Line         engine.PitcherLine `json:"line"`
}

// Version answers this module's version as the running program was built
// with it: the main module's when the program is this module's verifier, the
// dependency's when a site imports it, or "" when the build does not say.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if info.Main.Path == Path {
		return info.Main.Version
	}
	for _, d := range info.Deps {
		if d.Path == Path {
			return d.Version
		}
	}
	return ""
}

// Link is one checked link of a game's proof.
type Link struct {
	Name   string
	OK     bool
	None   bool // the link has nothing to check, such as a Day with no anchor
	Detail string
}

// Report is every link Check examined for one game.
type Report struct {
	GameID int64
	Links  []Link
}

// OK says every link passed.
func (r Report) OK() bool {
	for _, l := range r.Links {
		if !l.OK {
			return false
		}
	}
	return len(r.Links) > 0
}

// Anchored says the Day's commitment carried an anchor and it passed.
func (r Report) Anchored() bool {
	for _, l := range r.Links {
		if l.Name == "anchor" {
			return l.OK && !l.None
		}
	}
	return false
}

// Verifier checks a beacon round's signature and answers its randomness;
// beacon.Verify is the real one.
type Verifier func(round uint64, signature string) ([]byte, error)

// Check replays a game from its proof and checks every link: the engine
// model, the proof against its Day's commitment, the commitment's anchor
// under trust and its round after the anchor's time, the Day's inputs against
// their hash, the beacon round's signature and randomness, the seed, and the
// replay against the served box score. A proof from another engine model
// names the verifier version that replays it instead. A Day with no anchor
// reports that link as none, and the rest still decide the verdict.
func Check(p Proof, c Commitment, box BoxScore, verify Verifier, trust Trust) Report {
	r := Report{GameID: p.GameID}
	add := func(name string, ok bool, detail string, args ...any) {
		r.Links = append(r.Links, Link{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}

	engineOK := p.Engine == engine.Version
	if engineOK {
		add("engine", true, "model %s", p.Engine)
	} else {
		add("engine", false, "the game used engine model %s and this verifier replays model %s; %s", p.Engine, engine.Version, otherVerifier(p.Module))
	}

	switch {
	case c.BaseballLeague.ID != p.BaseballLeague.ID || c.Season != p.Season || c.Day != p.Day:
		add("commitment", false, "the commitment is for baseball league %d season %d Day %d, not the game's baseball league %d season %d Day %d", c.BaseballLeague.ID, c.Season, c.Day, p.BaseballLeague.ID, p.Season, p.Day)
	case c.Round != p.Round:
		add("commitment", false, "the game used round %d, not the Day's bound round %d", p.Round, c.Round)
	case c.InputsHash != p.InputsHash:
		add("commitment", false, "the proof names inputs hash %s, the commitment %s", p.InputsHash, c.InputsHash)
	default:
		add("commitment", true, "season %d Day %d, round %d, committed %s", c.Season, c.Day, c.Round, c.CommittedAt.UTC().Format(time.RFC3339))
	}

	if c.Anchor == nil {
		r.Links = append(r.Links, Link{Name: "anchor", OK: true, None: true, Detail: "the Day's commitment has no anchor"})
	} else if at, err := VerifyAnchor(*c.Anchor, p.BaseballLeagueSeed, c.Season, c.Day, c.InputsHash, trust); err != nil {
		add("anchor", false, "%v", err)
	} else if published := beacon.TimeOf(c.Round); !published.After(at) {
		add("anchor", false, "round %d was published %s, not after the anchor's time %s", c.Round, published.UTC().Format(time.RFC3339), at.Format(time.RFC3339))
	} else {
		add("anchor", true, "timestamped %s by %d authorities and logged at %s; round %d came after", at.Format(time.RFC3339), len(c.Anchor.Timestamps), c.Anchor.Log.URL, c.Round)
	}

	inputs, inputsOK := gameInputs(p, c, add)

	randomness, err := verify(p.Round, p.Signature)
	switch {
	case p.Chain != beacon.ChainHash:
		add("beacon", false, "the proof names chain %s, not quicknet %s", p.Chain, beacon.ChainHash)
	case err != nil:
		add("beacon", false, "round %d: %v", p.Round, err)
	case hex.EncodeToString(randomness) != p.Randomness:
		add("beacon", false, "round %d's randomness is %x, not the proof's %s", p.Round, randomness, p.Randomness)
	default:
		add("beacon", true, "round %d, published %s, signature verified", p.Round, beacon.TimeOf(p.Round).UTC().Format(time.RFC3339))
	}
	beaconOK := err == nil && p.Chain == beacon.ChainHash && hex.EncodeToString(randomness) == p.Randomness

	seedOK := p.Derivation == engine.DerivationBeacon && p.Game == uint64(p.Day*16+p.Slot)
	if seedOK {
		add("seed", true, "%s with baseball league seed %d, season %d, game %d", p.Derivation, p.BaseballLeagueSeed, p.Season, p.Game)
	} else {
		add("seed", false, "the proof names derivation %q and game %d; the beacon derivation %q and game %d (Day*16+slot) were expected", p.Derivation, p.Game, engine.DerivationBeacon, p.Day*16+p.Slot)
	}

	if !engineOK || !inputsOK || !beaconOK || !seedOK {
		add("replay", false, "not replayed until the links above pass")
		return r
	}
	res, err := engine.Simulate(inputs.Home, inputs.Away, engine.DeriveBeaconSeed(p.BaseballLeagueSeed, p.Season, p.Game, randomness))
	if err != nil {
		add("replay", false, "the replay failed: %v", err)
		return r
	}
	if problem := compareBox(res, box, p.GameID); problem != "" {
		add("replay", false, "%s", problem)
		return r
	}
	add("replay", true, "%d-%d, every inning, batter, and pitcher line matches the box score", res.Away.Runs, res.Home.Runs)
	return r
}

// gameInputs checks the Day's inputs against their hash and finds the game's
// own among them, answering them when both hold.
func gameInputs(p Proof, c Commitment, add func(string, bool, string, ...any)) (Pairing, bool) {
	var none Pairing
	sum, err := HashInputs(c.Inputs)
	if err != nil {
		add("inputs", false, "%v", err)
		return none, false
	}
	if sum != c.InputsHash {
		add("inputs", false, "the Day's inputs hash to %s, not the committed %s", sum, c.InputsHash)
		return none, false
	}
	var day []json.RawMessage
	if err := json.Unmarshal(c.Inputs, &day); err != nil || p.Slot < 0 || p.Slot >= len(day) {
		add("inputs", false, "the Day's inputs hold no game for slot %d", p.Slot)
		return none, false
	}
	var own, mine bytes.Buffer
	if json.Compact(&own, day[p.Slot]) != nil || json.Compact(&mine, p.Inputs) != nil || !bytes.Equal(own.Bytes(), mine.Bytes()) {
		add("inputs", false, "the proof's inputs differ from slot %d of the Day's committed inputs", p.Slot)
		return none, false
	}
	var pair Pairing
	if err := json.Unmarshal(day[p.Slot], &pair); err != nil {
		add("inputs", false, "slot %d's inputs: %v", p.Slot, err)
		return none, false
	}
	add("inputs", true, "the Day's %d games hash to the commitment, slot %d among them", len(day), p.Slot)
	return pair, true
}

// compareBox answers how a replay differs from a served box score, or "".
func compareBox(res engine.Result, box BoxScore, id int64) string {
	if box.ID != id {
		return fmt.Sprintf("the box score is game %d's, not game %d's", box.ID, id)
	}
	if box.Away.Runs != res.Away.Runs || box.Home.Runs != res.Home.Runs {
		return fmt.Sprintf("the replay scores %d-%d, the box score %d-%d", res.Away.Runs, res.Home.Runs, box.Away.Runs, box.Home.Runs)
	}
	batters, pitchers := 0, 0
	for i, side := range []BoxSide{box.Away, box.Home} {
		half := []string{"away", "home"}[i]
		if problem := compareInnings(half, side.Innings, res.LineScore[i]); problem != "" {
			return problem
		}
		hits := 0
		for n, b := range side.Batting {
			id, problem := lineID(b.EnginePlayer, b.Player)
			if problem != "" {
				return fmt.Sprintf("the %s side's batter line %d %s", half, n+1, problem)
			}
			line, ok := res.Batters[id]
			if !ok || line != b.Line {
				return fmt.Sprintf("batter %s's line differs from the replay's", id)
			}
			hits += line.Hits()
		}
		if hits != side.Hits {
			return fmt.Sprintf("the %s side's hits are %d in the box score and %d in the replay", half, side.Hits, hits)
		}
		for n, pl := range side.Pitching {
			id, problem := lineID(pl.EnginePlayer, pl.Player)
			if problem != "" {
				return fmt.Sprintf("the %s side's pitcher line %d %s", half, n+1, problem)
			}
			line, ok := res.Pitchers[id]
			if !ok || line != pl.Line {
				return fmt.Sprintf("pitcher %s's line differs from the replay's", id)
			}
		}
		batters += len(side.Batting)
		pitchers += len(side.Pitching)
	}
	if batters != len(res.Batters) || pitchers != len(res.Pitchers) {
		return fmt.Sprintf("the box score lists %d batters and %d pitchers, the replay %d and %d", batters, pitchers, len(res.Batters), len(res.Pitchers))
	}
	return ""
}

// lineID answers the id a box score line names its player by for the replay:
// engine_player, which must then be a nonempty JSON string, or, only when
// engine_player is absent, a nonempty string player, the shape box scores had
// before engine_player. Otherwise it answers why the line cannot be matched;
// an unusable engine_player never falls back to player.
func lineID(enginePlayer, player json.RawMessage) (id, problem string) {
	if enginePlayer = bytes.TrimSpace(enginePlayer); len(enginePlayer) > 0 {
		if json.Unmarshal(enginePlayer, &id) != nil || id == "" {
			return "", "has engine_player " + shown(enginePlayer) + ", not a player's id"
		}
		return id, ""
	}
	if json.Unmarshal(player, &id) != nil || id == "" {
		return "", "names no engine_player to match the replay"
	}
	return id, ""
}

// shown is a JSON value as a message quotes it: an object or an array by its
// kind, anything else as written, cut to 40 bytes.
func shown(raw json.RawMessage) string {
	switch raw[0] {
	case '{':
		return "an object"
	case '[':
		return "an array"
	}
	if len(raw) > 40 {
		return string(raw[:40]) + "..."
	}
	return string(raw)
}

// compareInnings answers how a side's served runs by inning differ from the
// replay's line score, where a served null is a half that was not played.
func compareInnings(half string, served []*int, replay []int) string {
	played := 0
	for i, v := range served {
		if v == nil {
			continue
		}
		if i >= len(replay) || *v != replay[i] {
			return fmt.Sprintf("the %s side's inning %d differs from the replay's", half, i+1)
		}
		played = i + 1
	}
	if played != len(replay) {
		return fmt.Sprintf("the %s side's box score shows %d innings, the replay %d", half, played, len(replay))
	}
	return ""
}

// otherVerifier answers the advice for a proof from another engine model: the
// command that runs the verifier version the proof names, or, when the proof
// names this verifier's own version, another module, or no version, a note
// that it names no other, so the advice never sends a reader back to the
// verifier that just failed.
func otherVerifier(module string) string {
	path, version, ok := strings.Cut(module, "@")
	if !ok || path != Path || version == "" || version == Version() {
		return "the proof names no other verifier version to run"
	}
	return "run " + RunCommand(module)
}

// RunCommand is the command that runs the verifier at the module version a
// proof names, PATH@VERSION, or this version's when the proof names none.
func RunCommand(module string) string {
	version := ""
	if path, v, ok := strings.Cut(module, "@"); ok && path == Path {
		version = v
	}
	if version == "" {
		version = "VERSION"
	}
	return "go run " + Path + "/cmd/mud-verify@" + version + " URL"
}
