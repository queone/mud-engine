package proof_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/queone/mud-engine/beacon"
	"github.com/queone/mud-engine/internal/prooftest"
	"github.com/queone/mud-engine/proof"
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
	r := proof.Check(p, c, box, beacon.Verify)
	if !r.OK() {
		t.Fatalf("an untouched game fails: %+v", r.Links)
	}
	var names []string
	for _, l := range r.Links {
		names = append(names, l.Name)
	}
	if got := strings.Join(names, " "); got != "engine commitment inputs beacon seed replay" {
		t.Errorf("links %q", got)
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
			r := proof.Check(p, c, box, beacon.Verify)
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
	for _, raw := range []string{"mudball.one/games/12", "https://mudball.one/circuits/1", "https://mudball.one/games/x", "https://mudball.one/games/0"} {
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
