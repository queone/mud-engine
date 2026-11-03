// Command mud-verify replays a game Mud Baseball played and checks its proof:
// the drand beacon round's signature, the seed, the Day's inputs against
// their commitment, and every line of the box score.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/queone/mud-engine/beacon"
	"github.com/queone/mud-engine/engine"
	"github.com/queone/mud-engine/proof"
)

const usage = `mud-verify replays a Mud Baseball game and checks every link of its proof.

Usage:
  mud-verify URL
  mud-verify -f PROOF -c COMMITMENT -b BOXSCORE

URL is a game's box score page, https://SITE/games/ID, or its API answer,
https://SITE/api/games/ID. The second form checks saved copies of the game's
/api/games/ID/proof, its Day's commitment, and /api/games/ID.

Options:
  -f, --proof FILE        a saved proof
  -c, --commitment FILE   the saved commitment of the game's Day
  -b, --box FILE          the saved box score
  -h, --help              this help

It exits 0 when every link passes, 1 when one fails or the proof cannot be
read, and 2 for a wrong command line.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, http.DefaultClient))
}

// run checks one game and answers the exit code.
func run(args []string, stdout, stderr io.Writer, client *http.Client) int {
	var address, proofFile, commitmentFile, boxFile string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-h", "-?", "--help", "help":
			fmt.Fprint(stdout, usage)
			return 0
		}
		if !strings.HasPrefix(a, "-") {
			if address != "" {
				fmt.Fprintf(stderr, "mud-verify: unexpected argument %q; run mud-verify --help\n", a)
				return 2
			}
			address = a
			continue
		}
		key, value, inline := strings.Cut(a, "=")
		if !inline {
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "mud-verify: flag %s needs a value; run mud-verify --help\n", key)
				return 2
			}
			i++
			value = args[i]
		}
		switch key {
		case "-f", "--proof":
			proofFile = value
		case "-c", "--commitment":
			commitmentFile = value
		case "-b", "--box":
			boxFile = value
		default:
			fmt.Fprintf(stderr, "mud-verify: unknown flag %s; run mud-verify --help\n", key)
			return 2
		}
	}
	offline := proofFile != "" || commitmentFile != "" || boxFile != ""
	if (address == "") == !offline || (offline && (proofFile == "" || commitmentFile == "" || boxFile == "")) {
		fmt.Fprintln(stderr, "mud-verify: give a game's URL, or all of -f, -c, and -b; run mud-verify --help")
		return 2
	}

	var (
		p   proof.Proof
		c   proof.Commitment
		box proof.BoxScore
		err error
	)
	if offline {
		err = readFiles(proofFile, &p, commitmentFile, &c, boxFile, &box)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		p, c, box, err = proof.Fetch(ctx, client, address)
	}
	if err != nil {
		fmt.Fprintf(stderr, "mud-verify: %v\n", err)
		return 1
	}

	report := proof.Check(p, c, box, beacon.Verify)
	fmt.Fprintf(stdout, "game %d, %s season %d Day %d, slot %d; verifier engine model %s\n", p.GameID, p.World.Name, p.Season, p.Day, p.Slot, engine.Version)
	for _, l := range report.Links {
		verdict := "passed"
		if !l.OK {
			verdict = "FAILED"
		}
		fmt.Fprintf(stdout, "  %-10s %s: %s\n", l.Name, verdict, l.Detail)
	}
	if !report.OK() {
		fmt.Fprintln(stdout, "not verified")
		return 1
	}
	fmt.Fprintln(stdout, "verified")
	return 0
}

// readFiles decodes the three saved answers.
func readFiles(pairs ...any) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		name := pairs[i].(string)
		raw, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if err := json.Unmarshal(raw, pairs[i+1]); err != nil {
			return fmt.Errorf("%s is not the expected JSON: %w", name, err)
		}
	}
	return nil
}
