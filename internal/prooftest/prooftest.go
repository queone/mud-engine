// Package prooftest builds one game's proof, Day commitment, and box score
// for tests, on a real recorded quicknet round, shaped as a site serves them.
package prooftest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/queone/mud-engine/beacon"
	"github.com/queone/mud-engine/engine"
	"github.com/queone/mud-engine/proof"
)

// A real quicknet round, recorded from api.drand.sh.
const (
	Round      = 33620529
	Signature  = "8c86700b0b3ecd2c45aa44a19feeb796914a58a7e0d6fefaad6364520787162a22f89a9fe87f132c9e7dc10ff389e1d7"
	Randomness = "b174b768d659aad91d60b6cfc867e54ec7659ca0ce13c91c92359892f0e364ef"
)

// The game's place: game 7, circuit 1, season 1, Day 3, slot 2.
const (
	GameID  = 7
	Circuit = 1
	Season  = 1
	Day     = 3
	Slot    = 2
	Seed    = 42
)

// Game builds the game's proof, its Day's commitment, and its box score; the
// Day holds three games and this one is slot 2.
func Game() (proof.Proof, proof.Commitment, proof.BoxScore, error) {
	var day []proof.Pairing
	for i := range 3 {
		day = append(day, proof.Pairing{Home: club(fmt.Sprintf("Home %d", i), fmt.Sprintf("h%d", i), 50+i), Away: club(fmt.Sprintf("Away %d", i), fmt.Sprintf("a%d", i), 50-i)})
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
	res, err := engine.Simulate(day[Slot].Home, day[Slot].Away, engine.DeriveBeaconSeed(Seed, Season, game, randomness))
	if err != nil {
		return proof.Proof{}, proof.Commitment{}, proof.BoxScore{}, err
	}
	committed := beacon.TimeOf(Round).Add(-24 * time.Hour).UTC()
	circuit := proof.Circuit{ID: Circuit, Name: "MFB"}
	p := proof.Proof{GameID: GameID, World: circuit, Season: Season, Day: Day, Slot: Slot, Game: game, CircuitSeed: Seed,
		Derivation: engine.DerivationBeacon, Chain: beacon.ChainHash, Scheme: beacon.Scheme, Round: Round, RoundAt: beacon.TimeOf(Round),
		Signature: Signature, Randomness: Randomness, InputsHash: hash, CommittedAt: committed, Inputs: own, Engine: engine.Version,
		Module: proof.Path + "@v0.1.0"}
	c := proof.Commitment{World: circuit, Season: Season, Day: Day, Round: Round, RoundAt: beacon.TimeOf(Round), InputsHash: hash, CommittedAt: committed, Inputs: inputs}
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
				side.Batting = append(side.Batting, proof.BoxBatter{Player: id, Line: line})
				side.Hits += line.Hits()
			}
		}
		for id, line := range res.Pitchers {
			if strings.HasPrefix(id, prefix) {
				side.Pitching = append(side.Pitching, proof.BoxPitcher{Player: id, Line: line})
			}
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
