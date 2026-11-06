package proof_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
		{"another engine model", "engine", "go run github.com/queone/mud-engine/cmd/mud-verify@v0.1.0 URL", func(p *proof.Proof, _ *proof.Commitment, _ *proof.BoxScore) {
			p.Engine = "1"
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

func anchored(t *testing.T) (proof.Proof, proof.Commitment, proof.BoxScore, *notary.Notary) {
	t.Helper()
	p, c, box, n, err := prooftest.AnchoredGame()
	if err != nil {
		t.Fatal(err)
	}
	return p, c, box, n
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
