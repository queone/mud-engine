package proof_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/queone/mud-engine/beacon"
	"github.com/queone/mud-engine/internal/prooftest"
	"github.com/queone/mud-engine/proof"
	"github.com/queone/mud-engine/proof/notary"
)

func game(t *testing.T) (proof.Proof, proof.Commitment, proof.BoxScore) {
	t.Helper()
	p, c, box, err := prooftest.Game()
	if err != nil {
		t.Fatal(err)
	}
	return p, c, box
}

// link answers the named link of a report.
func link(t *testing.T, r proof.Report, name string) proof.Link {
	t.Helper()
	for _, l := range r.Links {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no %s link in %+v", name, r.Links)
	return proof.Link{}
}

func TestAnUntouchedGamePassesEveryLink(t *testing.T) {
	p, c, box := game(t)
	r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust())
	if !r.OK() {
		t.Fatalf("an untouched game fails: %+v", r.Links)
	}
	var names []string
	for _, l := range r.Links {
		names = append(names, l.Name)
	}
	if got := strings.Join(names, " "); got != "engine commitment anchor inputs beacon seed replay" {
		t.Errorf("links %q", got)
	}
	if a := link(t, r, "anchor"); !a.None || r.Anchored() {
		t.Errorf("a Day with no anchor reads %+v, anchored %v", a, r.Anchored())
	}
}

func TestEachAlteredLinkFailsOnItsOwn(t *testing.T) {
	cases := []struct {
		name  string
		link  string
		want  string
		alter func(*proof.Proof, *proof.Commitment, *proof.BoxScore)
	}{
		{"an altered box score", "replay", "line differs", func(_ *proof.Proof, _ *proof.Commitment, b *proof.BoxScore) {
			b.Home.Batting[0].Line.HR++
		}},
		{"an altered score", "replay", "the replay scores", func(_ *proof.Proof, _ *proof.Commitment, b *proof.BoxScore) { b.Away.Runs += 3 }},
		{"altered inputs", "inputs", "not the committed", func(_ *proof.Proof, c *proof.Commitment, _ *proof.BoxScore) {
			c.Inputs = bytes.Replace(c.Inputs, []byte(`"contact":51`), []byte(`"contact":99`), 1)
		}},
		{"inputs swapped for another slot's", "inputs", "differ from slot", func(p *proof.Proof, c *proof.Commitment, _ *proof.BoxScore) {
			var day []json.RawMessage
			_ = json.Unmarshal(c.Inputs, &day)
			p.Inputs = day[0]
		}},
		{"a forged signature", "beacon", "does not verify", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) {
			p.Signature = "9" + p.Signature[1:]
		}},
		{"another round's randomness", "beacon", "not the proof's", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) {
			p.Randomness = strings.Repeat("0", 64)
		}},
		{"a round other than the Day's", "commitment", "bound round", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) { p.Round++ }},
		{"another game number", "seed", "Day*16+slot", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) { p.Game++ }},
		{"another engine model", "engine", "binds engine model 2, the proof names 1", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) {
			p.Engine = "1"
		}},
		{"another draw mode", "engine", "binds game draws, the proof names pitch", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) {
			p.Draws = "pitch"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, box := game(t)
			tc.alter(&p, &c, &box)
			r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust())
			if r.OK() {
				t.Fatal("the altered game passes")
			}
			l := link(t, r, tc.link)
			if l.OK || !strings.Contains(l.Detail, tc.want) {
				t.Errorf("%s link: %+v, want a failure naming %q", tc.link, l, tc.want)
			}
			if tc.link != "replay" && link(t, r, "replay").OK {
				t.Error("the game replayed though an earlier link failed")
			}
		})
	}
}

func TestTheInputsHashIgnoresIndentation(t *testing.T) {
	_, c, _ := game(t)
	var indented bytes.Buffer
	if err := json.Indent(&indented, c.Inputs, "", "  "); err != nil {
		t.Fatal(err)
	}
	a, err1 := proof.HashInputs(c.Inputs)
	b, err2 := proof.HashInputs(indented.Bytes())
	if err1 != nil || err2 != nil || a != b || a != c.InputsHash {
		t.Errorf("hashes %s and %s, committed %s (%v, %v)", a, b, c.InputsHash, err1, err2)
	}
	if _, err := proof.HashInputs([]byte("{")); err == nil {
		t.Error("broken JSON hashes")
	}
}

func TestGameAddressTakesThePageAndTheAPIForm(t *testing.T) {
	for _, raw := range []string{"https://mudball.one/games/12", "https://mudball.one/api/games/12", "https://mudball.one/api/games/12/proof", "https://mudball.one/games/12/"} {
		base, id, err := proof.GameAddress(raw)
		if err != nil || base != "https://mudball.one" || id != 12 {
			t.Errorf("%s: %q %d %v", raw, base, id, err)
		}
	}
	for _, raw := range []string{"mudball.one/games/12", "https://mudball.one/baseball leagues/1", "https://mudball.one/games/x", "https://mudball.one/games/0"} {
		if _, _, err := proof.GameAddress(raw); err == nil || !strings.Contains(err.Error(), "not a game's address") {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestRunCommandNamesTheProofsVersion(t *testing.T) {
	if got := proof.RunCommand("github.com/queone/mud-engine@v0.3.1"); got != "go run github.com/queone/mud-engine/cmd/mud-verify@v0.3.1 URL" {
		t.Error(got)
	}
	if got := proof.RunCommand(""); !strings.HasSuffix(got, "@VERSION URL") {
		t.Error(got)
	}
}

// The advice for a proof from another engine model names the verifier
// version the proof names, and never this verifier's own version, another
// module's, or none.
func TestTheEngineAdviceNeverNamesTheRunningVerifier(t *testing.T) {
	for module, command := range map[string]bool{
		proof.Path + "@v0.1.0":             true,
		proof.Path + "@" + proof.Version(): false,
		"example.com/other@v0.1.0":         false,
		proof.Path:                         false,
		"":                                 false,
	} {
		p, c, box := model3(t)
		if err := prooftest.WithModel(&p, &c, "1"); err != nil {
			t.Fatal(err)
		}
		p.Module = module
		l := link(t, proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust()), "engine")
		named := strings.Contains(l.Detail, "go run ")
		if l.OK || named != command || (!command && !strings.HasSuffix(l.Detail, "the proof names no other verifier version to run")) {
			t.Errorf("module %q: %q", module, l.Detail)
		}
	}
}

// decoded is a box score as a reader gets it: written as JSON text and read
// back, so the reader decodes the JSON types the text carries.
func decoded(t *testing.T, box proof.BoxScore) (proof.BoxScore, string) {
	t.Helper()
	raw, err := json.Marshal(box)
	if err != nil {
		t.Fatal(err)
	}
	var back proof.BoxScore
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("reading %s: %v", raw, err)
	}
	return back, string(raw)
}

// A box score whose player is a number beside a string engine_player, as a
// site that numbers its players serves it, and one saved before
// engine_player, with the id in a string player, both pass every link.
func TestBoxScoresMatchByEnginePlayerOrAnOlderStringPlayer(t *testing.T) {
	p, c, box := game(t)
	numbered, text := decoded(t, prooftest.Numbered(box))
	if !strings.Contains(text, `"player":1,`) || !strings.Contains(text, `"engine_player":"`) {
		t.Fatalf("the numbered box score's JSON: %s", text)
	}
	if r := proof.Check(p, c, numbered, beacon.Verify, proof.DefaultTrust()); !r.OK() {
		t.Errorf("a numbered box score fails: %+v", r.Links)
	}
	older, text := decoded(t, prooftest.Legacy(box))
	if strings.Contains(text, "engine_player") || !strings.Contains(text, `"player":"`) {
		t.Fatalf("the older box score's JSON: %s", text)
	}
	if r := proof.Check(p, c, older, beacon.Verify, proof.DefaultTrust()); !r.OK() {
		t.Errorf("an older box score fails: %+v", r.Links)
	}
}

// A line the replay cannot be matched to fails the replay link alone, naming
// its side, kind, and number and no verifier version, and the game is not
// verified; an engine_player that is present but not a nonempty string never
// falls back to a valid older player.
func TestALineWithNoUsableIDFailsTheReplayLinkAlone(t *testing.T) {
	cases := []struct {
		name         string
		pitcher      bool
		enginePlayer string // absent when empty
		player       string // absent when empty; "id" is the line's own id as a string
		want         string
	}{
		{"a number in player and no engine_player", false, "", "5", "the home side's batter line 1 names no engine_player to match the replay"},
		{"neither field", true, "", "", "the home side's pitcher line 1 names no engine_player to match the replay"},
		{"an empty engine_player", false, `""`, "id", `the home side's batter line 1 has engine_player "", not a player's id`},
		{"a null engine_player", false, "null", "id", "the home side's batter line 1 has engine_player null, not a player's id"},
		{"a number in engine_player", true, "7", "id", "the home side's pitcher line 1 has engine_player 7, not a player's id"},
		{"a boolean engine_player", false, "true", "id", "the home side's batter line 1 has engine_player true, not a player's id"},
		{"an object engine_player", false, `{"id": "x"}`, "id", "the home side's batter line 1 has engine_player an object, not a player's id"},
		{"an array engine_player", false, `["x"]`, "id", "the home side's batter line 1 has engine_player an array, not a player's id"},
		{"an engine_player the replay lacks", false, `"zz"`, "", "batter zz's line differs from the replay's"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, box := game(t)
			box = prooftest.Numbered(box)
			enginePlayer, player := &box.Home.Batting[0].EnginePlayer, &box.Home.Batting[0].Player
			if tc.pitcher {
				enginePlayer, player = &box.Home.Pitching[0].EnginePlayer, &box.Home.Pitching[0].Player
			}
			own := *enginePlayer
			*enginePlayer, *player = nil, nil
			if tc.enginePlayer != "" {
				*enginePlayer = json.RawMessage(tc.enginePlayer)
			}
			switch tc.player {
			case "":
			case "id":
				*player = own
			default:
				*player = json.RawMessage(tc.player)
			}
			read, text := decoded(t, box)
			r := proof.Check(p, c, read, beacon.Verify, proof.DefaultTrust())
			if r.OK() {
				t.Fatalf("the game verifies: %s", text)
			}
			if replay := link(t, r, "replay"); replay.OK || !strings.Contains(replay.Detail, tc.want) || strings.Contains(replay.Detail, "go run") {
				t.Errorf("replay link %q, want %q and no verifier version", replay.Detail, tc.want)
			}
			for _, l := range r.Links {
				if l.Name != "replay" && !l.OK {
					t.Errorf("the %s link fails too: %s", l.Name, l.Detail)
				}
			}
		})
	}
}

func anchored(t *testing.T) (proof.Proof, proof.Commitment, proof.BoxScore, *notary.Notary) {
	t.Helper()
	p, c, box, n, err := prooftest.AnchoredGame()
	if err != nil {
		t.Fatal(err)
	}
	return p, c, box, n
}

// A game played under its own commitment passes every link, the anchor's
// included when its commitment is anchored.
func TestAGameUnderItsOwnCommitmentPassesEveryLink(t *testing.T) {
	p, c, box, err := prooftest.OwnGame()
	if err != nil {
		t.Fatal(err)
	}
	r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust())
	if !r.OK() || len(r.Links) != 7 || !strings.Contains(link(t, r, "commitment").Detail, "the game's own, slot 2") {
		t.Fatalf("a game under its own commitment: %+v", r.Links)
	}
	if a := link(t, r, "anchor"); !a.None || a.Detail != "the game's commitment has no anchor" {
		t.Errorf("anchor link %+v", a)
	}
	p, c, box, n, err := prooftest.AnchoredOwnGame()
	if err != nil {
		t.Fatal(err)
	}
	if r := proof.Check(p, c, box, beacon.Verify, n.Trust); !r.OK() || !r.Anchored() {
		t.Fatalf("an anchored game under its own commitment: %+v", r.Links)
	}
}

// Every way a game's own commitment can be wrong fails its link.
func TestEachMismatchOfAGamesOwnCommitmentFailsItsLink(t *testing.T) {
	cases := []struct {
		name, link, want string
		alter            func(*proof.Proof, *proof.Commitment)
	}{
		{"a commitment for another slot", "commitment", "is for slot 1, not the game's slot 2", func(_ *proof.Proof, c *proof.Commitment) { c.Slot = 1 }},
		{"another round", "commitment", "not the game's bound round", func(_ *proof.Proof, c *proof.Commitment) { c.Round++ }},
		{"a proof that names its Day's commitment", "commitment", "names a day commitment, but the site served a game one", func(p *proof.Proof, _ *proof.Commitment) { p.CommitmentScope = "" }},
		{"inputs that are not the committed ones", "inputs", "hash to", func(_ *proof.Proof, c *proof.Commitment) {
			c.Inputs = bytes.Replace(c.Inputs, []byte(`"contact":52`), []byte(`"contact":99`), 1)
		}},
		{"no scheduled start", "commitment", "names no scheduled start", func(_ *proof.Proof, c *proof.Commitment) { c.Start = time.Time{} }},
		{"a scheduled start whose first round is another", "commitment", "is not the first round at or after the game's start", func(_ *proof.Proof, c *proof.Commitment) {
			c.Start = c.Start.Add(-time.Hour)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, box, err := prooftest.OwnGame()
			if err != nil {
				t.Fatal(err)
			}
			tc.alter(&p, &c)
			r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust())
			if l := link(t, r, tc.link); r.OK() || l.OK || !strings.Contains(l.Detail, tc.want) {
				t.Errorf("%s link %+v, want a failure naming %q", tc.link, l, tc.want)
			}
		})
	}
	// An anchor for another game of the Day fails the anchor link.
	p, c, box, n, err := prooftest.AnchoredOwnGame()
	if err != nil {
		t.Fatal(err)
	}
	other, err := n.Anchor(proof.GameStatement(prooftest.Seed, prooftest.Season, prooftest.Day, 1, c.Start, c.InputsHash), c.CommittedAt)
	if err != nil {
		t.Fatal(err)
	}
	c.Anchor = &other
	if r := proof.Check(p, c, box, beacon.Verify, n.Trust); r.OK() || link(t, r, "anchor").OK || !strings.Contains(link(t, r, "anchor").Detail, "not this game's") {
		t.Errorf("an anchor for another game: %+v", link(t, r, "anchor"))
	}
	// A commitment that names another start than its anchor's statement fails
	// the anchor link.
	p, c, box, n, err = prooftest.AnchoredOwnGame()
	if err != nil {
		t.Fatal(err)
	}
	c.Start = c.Start.Add(time.Minute)
	if r := proof.Check(p, c, box, beacon.Verify, n.Trust); r.OK() || link(t, r, "anchor").OK || !strings.Contains(link(t, r, "anchor").Detail, "not this game's") {
		t.Errorf("another scheduled start: %+v", link(t, r, "anchor"))
	}
}

// An anchored game binds exactly the first round at or after its scheduled
// start that follows its anchor: a later round fails the anchor link even
// though it was published after the anchor.
func TestAGameBindsExactlyTheFirstRoundItsRuleAllows(t *testing.T) {
	p, c, box, n, err := prooftest.AnchoredOwnGame()
	if err != nil {
		t.Fatal(err)
	}
	if a := link(t, proof.Check(p, c, box, beacon.Verify, n.Trust), "anchor"); !a.OK || !strings.Contains(a.Detail, "is the first at or after the game's start") {
		t.Fatalf("the rule's own round: %+v", a)
	}
	p.Round, c.Round = p.Round+1, c.Round+1
	a := link(t, proof.Check(p, c, box, beacon.Verify, n.Trust), "anchor")
	if a.OK || !strings.Contains(a.Detail, fmt.Sprintf("round %d is not the first round at or after the game's start", c.Round)) || !strings.HasSuffix(a.Detail, fmt.Sprintf("round %d", prooftest.Round)) {
		t.Errorf("a later round: %+v", a)
	}
	// Anchored after its start, a game binds the first round after the anchor.
	start := beacon.TimeOf(prooftest.Round).Add(-time.Hour)
	if got := proof.GameRound(start, beacon.TimeOf(prooftest.Round)); got != prooftest.Round+1 {
		t.Errorf("anchored at round %d's moment after the start, the game binds round %d, want %d", prooftest.Round, got, prooftest.Round+1)
	}
	if got := proof.GameRound(start, beacon.TimeOf(prooftest.Round).Add(-time.Second)); got != prooftest.Round {
		t.Errorf("anchored a second before round %d, the game binds round %d", prooftest.Round, got)
	}
	if got := proof.GameRound(beacon.TimeOf(prooftest.Round), time.Time{}); got != prooftest.Round {
		t.Errorf("with no anchor, a game starting at round %d's moment binds round %d", prooftest.Round, got)
	}
}

func TestAnAnchoredGamePassesEverySevenLinks(t *testing.T) {
	p, c, box, n := anchored(t)
	r := proof.Check(p, c, box, beacon.Verify, n.Trust)
	if !r.OK() || !r.Anchored() || len(r.Links) != 7 {
		t.Fatalf("an anchored game: ok %v, anchored %v: %+v", r.OK(), r.Anchored(), r.Links)
	}
	if a := link(t, r, "anchor"); !strings.Contains(a.Detail, "by 2 authorities") || !strings.Contains(a.Detail, "came after") {
		t.Errorf("anchor link %q", a.Detail)
	}
	if r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust()); r.OK() || link(t, r, "anchor").OK {
		t.Error("an anchor from unpinned services passes under the public trust")
	}
}

// Every way an anchor can be wrong fails the anchor link alone; the replay
// still runs, since the anchor speaks to when, not what.
func TestEachAlteredAnchorFailsItsLink(t *testing.T) {
	cases := []struct {
		name  string
		want  string
		alter func(t *testing.T, c *proof.Commitment, n *notary.Notary)
	}{
		{"a token over another digest", "not the digest", func(t *testing.T, c *proof.Commitment, n *notary.Notary) {
			other := sha256.Sum256([]byte("another Day"))
			token, err := n.Token(0, other[:], c.Anchor.Timestamps[0].Time)
			if err != nil {
				t.Fatal(err)
			}
			c.Anchor.Timestamps[0].Token = base64.StdEncoding.EncodeToString(token)
		}},
		{"a token from an unpinned authority", "chains to no pinned", func(t *testing.T, c *proof.Commitment, _ *notary.Notary) {
			stranger, err := notary.New()
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := hex.DecodeString(c.Anchor.Digest)
			token, err := stranger.Token(0, digest, c.Anchor.Timestamps[0].Time)
			if err != nil {
				t.Fatal(err)
			}
			c.Anchor.Timestamps[0].Token = base64.StdEncoding.EncodeToString(token)
		}},
		{"one authority only", "two are needed", func(_ *testing.T, c *proof.Commitment, _ *notary.Notary) {
			c.Anchor.Timestamps = c.Anchor.Timestamps[1:]
		}},
		{"a statement for another Day", "not this Day's", func(_ *testing.T, c *proof.Commitment, _ *notary.Notary) {
			c.Anchor.Statement = string(proof.Statement(prooftest.Seed, prooftest.Season, prooftest.Day+1, c.InputsHash))
		}},
		{"a broken inclusion proof", "is not in", func(_ *testing.T, c *proof.Commitment, _ *notary.Notary) {
			c.Anchor.Log.Entry = editEntry(t, c.Anchor.Log.Entry, func(e map[string]any) {
				hashes := e["inclusionProof"].(map[string]any)["hashes"].([]any)
				hashes[0] = base64.StdEncoding.EncodeToString(make([]byte, 32))
			})
		}},
		{"a checkpoint the log did not sign", "not signed by", func(_ *testing.T, c *proof.Commitment, _ *notary.Notary) {
			c.Anchor.Log.Entry = editEntry(t, c.Anchor.Log.Entry, func(e map[string]any) {
				cp := e["inclusionProof"].(map[string]any)["checkpoint"].(map[string]any)
				lines := strings.SplitN(cp["envelope"].(string), "\n", 3)
				cp["envelope"] = lines[0] + "\n999\n" + lines[2]
			})
		}},
		{"an entry under another key", "another key", func(t *testing.T, c *proof.Commitment, n *notary.Notary) {
			stranger, err := notary.New()
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := hex.DecodeString(c.Anchor.Digest)
			body, err := notary.Body(digest, stranger.Key)
			if err != nil {
				t.Fatal(err)
			}
			if c.Anchor.Log.Entry, err = n.Enter(body); err != nil {
				t.Fatal(err)
			}
		}},
		{"a round published before the anchor", "not after the anchor", func(t *testing.T, c *proof.Commitment, n *notary.Notary) {
			late, err := n.Anchor([]byte(c.Anchor.Statement), beacon.TimeOf(c.Round).Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			c.Anchor = &late
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, box, n := anchored(t)
			tc.alter(t, &c, n)
			r := proof.Check(p, c, box, beacon.Verify, n.Trust)
			a := link(t, r, "anchor")
			if r.OK() || a.OK || !strings.Contains(a.Detail, tc.want) {
				t.Errorf("anchor link %+v, want a failure naming %q", a, tc.want)
			}
			if !link(t, r, "replay").OK {
				t.Error("an anchor failure stopped the replay")
			}
		})
	}
}

// editEntry rewrites a log entry's JSON.
func editEntry(t *testing.T, raw json.RawMessage, edit func(map[string]any)) json.RawMessage {
	t.Helper()
	var e map[string]any
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	edit(e)
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func model3(t *testing.T) (proof.Proof, proof.Commitment, proof.BoxScore) {
	t.Helper()
	p, c, box, err := prooftest.Model3Game()
	if err != nil {
		t.Fatal(err)
	}
	return p, c, box
}

func live(t *testing.T) (proof.Proof, proof.Commitment, proof.BoxScore) {
	t.Helper()
	p, c, box, err := prooftest.LiveGame()
	if err != nil {
		t.Fatal(err)
	}
	return p, c, box
}

// A model 3 game verifies in both draw modes through the model its
// commitment names, a per-pitch game's rounds checked as an eighth link, and
// a model 2 game still verifies through the frozen model.
func TestEachModelAndDrawModeReplaysAsCommitted(t *testing.T) {
	p, c, box := game(t)
	if r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust()); !r.OK() || link(t, r, "engine").Detail != "model 2, game draws" {
		t.Fatalf("a model 2 game: %+v", r.Links)
	}
	p, c, box = model3(t)
	r := proof.Check(p, c, box, beacon.Verify, proof.DefaultTrust())
	if !r.OK() || len(r.Links) != 7 || link(t, r, "engine").Detail != "model 3, game draws" {
		t.Fatalf("a model 3 game: %+v", r.Links)
	}
	p, c, box = live(t)
	r = proof.Check(p, c, box, prooftest.FakeVerify, proof.DefaultTrust())
	if !r.OK() || len(r.Links) != 8 || link(t, r, "engine").Detail != "model 3, pitch draws" {
		t.Fatalf("a live game: %+v", r.Links)
	}
	if l := link(t, r, "rounds"); !strings.HasSuffix(l.Detail, "rounds, every signature verified") || len(p.Rounds) < 200 {
		t.Errorf("rounds link %+v over %d rounds", l, len(p.Rounds))
	}
	if l := link(t, r, "replay"); !strings.Contains(l.Detail, fmt.Sprintf("on %d pitches", len(p.Rounds))) {
		t.Errorf("replay link %+v", l)
	}
	var names []string
	for _, l := range r.Links {
		names = append(names, l.Name)
	}
	if got := strings.Join(names, " "); got != "engine commitment anchor inputs beacon rounds seed replay" {
		t.Errorf("links %q", got)
	}
}

// A proof must name the model and draw mode its commitment bound, a
// commitment naming none binding model 2 and whole-game draws; a committed
// model this verifier does not replay names the verifier that does.
func TestAProofMustMatchItsCommittedModelAndDraws(t *testing.T) {
	cases := []struct {
		name, want string
		game       func(*testing.T) (proof.Proof, proof.Commitment, proof.BoxScore)
		alter      func(*proof.Proof, *proof.Commitment)
	}{
		{"model 2 claimed on a model 3 commitment", "binds engine model 3, the proof names 2", model3, func(p *proof.Proof, _ *proof.Commitment) { p.Engine = "2" }},
		{"pitch draws claimed on a game commitment", "binds game draws, the proof names pitch", model3, func(p *proof.Proof, _ *proof.Commitment) { p.Draws = "pitch" }},
		{"game draws claimed on a pitch commitment", "binds pitch draws, the proof names game", live, func(p *proof.Proof, _ *proof.Commitment) { p.Draws = "" }},
		{"model 3 claimed on an older commitment", "binds engine model 2, the proof names 3", game, func(p *proof.Proof, _ *proof.Commitment) { p.Engine = "3" }},
		{"a model this verifier does not replay", "replays models 2 and 3; run go run github.com/queone/mud-engine/cmd/mud-verify@v0.1.0 URL", model3, func(p *proof.Proof, c *proof.Commitment) {
			if err := prooftest.WithModel(p, c, "4"); err != nil {
				t.Fatal(err)
			}
			p.Module = proof.Path + "@v0.1.0"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, box := tc.game(t)
			tc.alter(&p, &c)
			r := proof.Check(p, c, box, prooftest.FakeVerify, proof.DefaultTrust())
			l := link(t, r, "engine")
			if r.OK() || l.OK || !strings.Contains(l.Detail, tc.want) {
				t.Errorf("engine link %+v, want a failure naming %q", l, tc.want)
			}
			if link(t, r, "replay").OK {
				t.Error("the game replayed though the engine link failed")
			}
		})
	}
}

// A live game's rounds are checked by signature, by sequence from the bound
// round under the gap rule, and by count, and a changed line still fails the
// replay.
func TestALiveGamesRoundsAreCheckedBySignatureSequenceAndCount(t *testing.T) {
	cases := []struct {
		name, link, want string
		alter            func(*proof.Proof, *proof.BoxScore)
	}{
		{"a forged signature", "rounds", "pitch 8's round", func(p *proof.Proof, _ *proof.BoxScore) { p.Rounds[7].Signature = "00" + p.Rounds[7].Signature[2:] }},
		{"a moved round", "replay", "pitch 6 lists round", func(p *proof.Proof, _ *proof.BoxScore) {
			p.Rounds[5].Round++
			p.Rounds[5].Signature = prooftest.FakeSignature(p.Rounds[5].Round)
		}},
		{"a missing round", "replay", "names none for pitch", func(p *proof.Proof, _ *proof.BoxScore) { p.Rounds = p.Rounds[:len(p.Rounds)-1] }},
		{"an extra round", "replay", "the game threw", func(p *proof.Proof, _ *proof.BoxScore) {
			last := p.Rounds[len(p.Rounds)-1].Round + 5
			p.Rounds = append(p.Rounds, proof.PitchRound{Round: last, Signature: prooftest.FakeSignature(last)})
		}},
		{"no rounds", "rounds", "lists no rounds", func(p *proof.Proof, _ *proof.BoxScore) { p.Rounds = nil }},
		{"another first round", "rounds", "not the bound round", func(p *proof.Proof, _ *proof.BoxScore) {
			p.Rounds[0] = proof.PitchRound{Round: p.Round + 1, Signature: prooftest.FakeSignature(p.Round + 1)}
		}},
		{"a changed line", "replay", "line differs", func(_ *proof.Proof, b *proof.BoxScore) { b.Away.Batting[0].Line.K++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, box := live(t)
			tc.alter(&p, &box)
			r := proof.Check(p, c, box, prooftest.FakeVerify, proof.DefaultTrust())
			l := link(t, r, tc.link)
			if r.OK() || l.OK || !strings.Contains(l.Detail, tc.want) {
				t.Errorf("%s link %+v, want a failure naming %q", tc.link, l, tc.want)
			}
		})
	}
}
