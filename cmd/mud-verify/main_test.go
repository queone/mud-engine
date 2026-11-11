package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/queone/mud-engine/internal/prooftest"
	"github.com/queone/mud-engine/proof"
)

// site serves one game the way the site does, indented JSON included.
func site(t *testing.T, p proof.Proof, c proof.Commitment, box proof.BoxScore) *httptest.Server {
	t.Helper()
	serve := func(v any) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			_ = enc.Encode(v)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("GET /api/games/%d/proof", p.GameID), serve(p))
	mux.HandleFunc(fmt.Sprintf("GET /api/baseball-leagues/%d/seasons/%d/days/%d/commitment", c.BaseballLeague.ID, c.Season, c.Day), serve(c))
	mux.HandleFunc(fmt.Sprintf("GET /api/games/%d/commitment", p.GameID), serve(c))
	mux.HandleFunc(fmt.Sprintf("GET /api/games/%d", p.GameID), serve(box))
	mux.HandleFunc("GET /api/games/404/proof", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "no game with id \"404\""}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func verify(t *testing.T, client *http.Client, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut, client)
	return out.String(), errOut.String(), code
}

func TestVerifyPassesAnUntouchedGameFromEitherAddress(t *testing.T) {
	p, c, box, err := prooftest.Game()
	if err != nil {
		t.Fatal(err)
	}
	ts := site(t, p, c, box)
	for _, path := range []string{"/games/7", "/api/games/7"} {
		out, errOut, code := verify(t, ts.Client(), ts.URL+path)
		if code != 0 || errOut != "" || !strings.HasSuffix(out, "\nverified, not anchored\n") || strings.Count(out, " passed: ") != 6 || !strings.Contains(out, "  anchor     none: ") {
			t.Errorf("%s: %d %q %q", path, code, out, errOut)
		}
	}
}

func TestVerifyFailsAnAlteredGameOnlineAndOffline(t *testing.T) {
	p, c, box, err := prooftest.Game()
	if err != nil {
		t.Fatal(err)
	}
	box.Away.Pitching[0].Line.K++
	ts := site(t, p, c, box)
	out, _, code := verify(t, ts.Client(), ts.URL+"/games/7")
	if code != 1 || !strings.Contains(out, "replay     FAILED: pitcher") || !strings.HasSuffix(out, "not verified\n") {
		t.Errorf("online: %d %q", code, out)
	}

	dir := t.TempDir()
	files := map[string]any{"proof.json": p, "commitment.json": c, "box.json": box}
	for name, v := range files {
		raw, _ := json.MarshalIndent(v, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	offline, _, code := verify(t, nil, "-f", filepath.Join(dir, "proof.json"), "--commitment", filepath.Join(dir, "commitment.json"), "-b="+filepath.Join(dir, "box.json"))
	if code != 1 || offline != out {
		t.Errorf("offline: %d %q, online gave %q", code, offline, out)
	}

	p2, c2, box2, _ := prooftest.Game()
	for name, v := range map[string]any{"proof.json": p2, "commitment.json": c2, "box.json": box2} {
		raw, _ := json.Marshal(v)
		_ = os.WriteFile(filepath.Join(dir, name), raw, 0o600)
	}
	if out, _, code := verify(t, nil, "-f", filepath.Join(dir, "proof.json"), "-c", filepath.Join(dir, "commitment.json"), "-b", filepath.Join(dir, "box.json")); code != 0 || !strings.HasSuffix(out, "\nverified, not anchored\n") {
		t.Errorf("offline untouched: %d %q", code, out)
	}
}

// save writes a game's three answers as the files mud-verify -f -c -b reads,
// and answers those arguments; the box score's file is the last.
func save(t *testing.T, p proof.Proof, c proof.Commitment, box proof.BoxScore) []string {
	t.Helper()
	dir := t.TempDir()
	var args []string
	for _, f := range []struct {
		flag, name string
		v          any
	}{{"-f", "proof.json", p}, {"-c", "commitment.json", c}, {"-b", "box.json", box}} {
		raw, err := json.MarshalIndent(f.v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, f.flag, path)
	}
	return args
}

// A box score whose player is a number beside a string engine_player, read
// from the JSON a site serves, verifies online and offline, and so does one
// saved before engine_player, with the id in a string player.
func TestVerifyReadsANumberedBoxScoreAndASavedOlderOne(t *testing.T) {
	p, c, box, err := prooftest.Game()
	if err != nil {
		t.Fatal(err)
	}
	numbered := prooftest.Numbered(box)
	ts := site(t, p, c, numbered)
	if out, errOut, code := verify(t, ts.Client(), ts.URL+"/games/7"); code != 0 || errOut != "" || !strings.HasSuffix(out, "\nverified, not anchored\n") {
		t.Errorf("online: %d %q %q", code, out, errOut)
	}
	for _, tc := range []struct {
		name      string
		box       proof.BoxScore
		has, lack string
	}{
		{"numbered", numbered, `"player": 1,`, ""},
		{"older", prooftest.Legacy(box), `"player": "`, "engine_player"},
	} {
		args := save(t, p, c, tc.box)
		raw, _ := os.ReadFile(args[len(args)-1])
		if !strings.Contains(string(raw), tc.has) || (tc.lack != "" && strings.Contains(string(raw), tc.lack)) {
			t.Fatalf("the saved %s box score: %s", tc.name, raw)
		}
		if out, errOut, code := verify(t, nil, args...); code != 0 || !strings.HasSuffix(out, "\nverified, not anchored\n") {
			t.Errorf("the saved %s box score offline: %d %q %q", tc.name, code, out, errOut)
		}
	}
}

// An engine_player that is not a player's id fails its line's replay check,
// online and offline; the other links still report, and the game is not
// verified.
func TestVerifyFailsAnUnusableEnginePlayerOnItsLine(t *testing.T) {
	p, c, box, err := prooftest.Game()
	if err != nil {
		t.Fatal(err)
	}
	bad := prooftest.Numbered(box)
	bad.Away.Batting[1].EnginePlayer = json.RawMessage("7")
	ts := site(t, p, c, bad)
	out, errOut, code := verify(t, ts.Client(), ts.URL+"/games/7")
	if code != 1 || errOut != "" || !strings.Contains(out, "  replay     FAILED: the away side's batter line 2 has engine_player 7, not a player's id\n") ||
		strings.Count(out, " passed: ") != 5 || !strings.HasSuffix(out, "\nnot verified\n") {
		t.Errorf("online: %d %q %q", code, out, errOut)
	}
	if offline, _, code := verify(t, nil, save(t, p, c, bad)...); code != 1 || offline != out {
		t.Errorf("offline: %d %q, online gave %q", code, offline, out)
	}
}

// A game played under its own commitment verifies from its address, the
// commitment fetched from the game's own route, and from saved copies.
func TestVerifyReadsAGamesOwnCommitmentOnlineAndOffline(t *testing.T) {
	p, c, box, err := prooftest.OwnGame()
	if err != nil {
		t.Fatal(err)
	}
	ts := site(t, p, c, box)
	out, errOut, code := verify(t, ts.Client(), ts.URL+"/games/7")
	if code != 0 || errOut != "" || !strings.HasSuffix(out, "\nverified, not anchored\n") || !strings.Contains(out, "the game's own, slot 2") {
		t.Errorf("online: %d %q %q", code, out, errOut)
	}
	if offline, _, code := verify(t, nil, save(t, p, c, box)...); code != 0 || offline != out {
		t.Errorf("offline: %d %q, online gave %q", code, offline, out)
	}
}

func TestVerifyNamesTheSitesErrorAndWrongCommandLines(t *testing.T) {
	p, c, box, _ := prooftest.Game()
	ts := site(t, p, c, box)
	if _, errOut, code := verify(t, ts.Client(), ts.URL+"/games/404"); code != 1 || !strings.Contains(errOut, `answered 404: no game with id "404"`) {
		t.Errorf("a missing game: %d %q", code, errOut)
	}
	for _, args := range [][]string{{}, {"a", "b"}, {"-x", "1"}, {"-f"}, {"-f", "p.json"}, {ts.URL + "/games/7", "-f", "p.json", "-c", "c", "-b", "b"}} {
		if _, errOut, code := verify(t, ts.Client(), args...); code != 2 || !strings.Contains(errOut, "mud-verify --help") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
	if out, _, code := verify(t, nil, "--help"); code != 0 || !strings.Contains(out, "mud-verify -f PROOF -c COMMITMENT -b BOXSCORE") {
		t.Errorf("help: %d %q", code, out)
	}
	if _, errOut, code := verify(t, nil, "-f", "missing.json", "-c", "c", "-b", "b"); code != 1 || !strings.Contains(errOut, "reading missing.json") {
		t.Errorf("a missing file: %d %q", code, errOut)
	}
}

// The version and help follow the shape the build checks, and the README's
// Usage block is the help itself.
func TestVersionAndHelpFollowTheBuildsShape(t *testing.T) {
	for _, flag := range []string{"-v", "--version"} {
		if out, errOut, code := verify(t, nil, flag); code != 0 || errOut != "" || out != "mud-verify v"+programVersion+"\n" {
			t.Errorf("%s: %d %q %q", flag, code, out, errOut)
		}
	}
	help, _, _ := verify(t, nil, "-h")
	for _, flag := range []string{"-?", "--help", "help"} {
		if out, errOut, code := verify(t, nil, flag); code != 0 || errOut != "" || out != help {
			t.Errorf("%s: %d %q gave other help than -h", flag, code, errOut)
		}
	}
	lines := strings.Split(help, "\n")
	if lines[0] != "mud-verify v"+programVersion || lines[1] == "" || strings.HasSuffix(lines[1], ".") ||
		lines[2] != "github.com/queone/mud-engine/cmd/mud-verify" || lines[3] != "" || lines[4] != "Usage" {
		t.Errorf("help opens %q", lines[:5])
	}
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, block, _ := strings.Cut(string(raw), "### Usage\n\n```text\n")
	block, _, _ = strings.Cut(block, "```\n")
	if block != help {
		t.Errorf("README.md's Usage block differs from the help:\n%s", block)
	}
}
