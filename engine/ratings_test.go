package engine

import (
	"strings"
	"testing"
)

func TestValidateAcceptsAverageAndTwoWayClubs(t *testing.T) {
	if err := averageClub("Home", "h").Validate(); err != nil {
		t.Fatal(err)
	}
	c := averageClub("Home", "h")
	c.Lineup[8].Player.ID = c.Starter.ID // the starter also bats as the DH
	if err := c.Validate(); err != nil {
		t.Fatalf("two-way player rejected: %v", err)
	}
}

func TestValidateNamesClubSlotAndField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *Club)
		want   []string
	}{
		{"rating zero", func(c *Club) { c.Lineup[0].Player.Batting.Contact = 0 }, []string{`club "Home"`, "lineup slot 1", "Contact 0"}},
		{"rating one hundred", func(c *Club) { c.Starter.Pitching.Stuff = 100 }, []string{`club "Home"`, "starter", "Stuff 100"}},
		{"missing bats", func(c *Club) { c.Lineup[1].Player.Bats = "" }, []string{"lineup slot 2", `bats ""`}},
		{"switch pitcher", func(c *Club) { c.Bullpen.Closer.Throws = Switch }, []string{"reliever 1", `throws "S" is not L or R`}},
		{"no DH", func(c *Club) { c.Lineup[8].Position = "RF" }, []string{"0 DH slots"}},
		{"no catcher", func(c *Club) { c.Lineup[0].Position = "1B" }, []string{"0 C slots"}},
		{"bad position", func(c *Club) { c.Lineup[3].Position = "P" }, []string{"lineup slot 4", `position "P"`}},
		{"missing starter", func(c *Club) { c.Starter = Player{} }, []string{"starter", "no id"}},
		{"small bullpen", func(c *Club) { c.Bullpen.Others = nil }, []string{"bullpen has 2 relievers", "need at least 3"}},
		{"duplicate lineup id", func(c *Club) { c.Lineup[2].Player.ID = c.Lineup[1].Player.ID }, []string{"lineup slot 3", "used twice"}},
		{"duplicate pitcher id", func(c *Club) { c.Bullpen.Others[0].ID = c.Starter.ID }, []string{"reliever 3", "used twice"}},
		{"no batting ratings", func(c *Club) { c.Lineup[0].Player.Batting = nil }, []string{"lineup slot 1", "no batting ratings"}},
		{"no pitching ratings", func(c *Club) { c.Bullpen.Setup.Pitching = nil }, []string{"reliever 2", "no pitching ratings"}},
		{"no name", func(c *Club) { c.Name = "" }, []string{"club has no name"}},
	}
	for _, c := range cases {
		club := averageClub("Home", "h")
		c.mutate(&club)
		err := club.Validate()
		if err == nil {
			t.Errorf("%s: Validate() = nil, want an error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q lacks %q", c.name, err, w)
			}
		}
	}
}

func TestSimulateRejectsAnIDSharedAcrossClubs(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	away.Lineup[4].Player.ID = home.Lineup[4].Player.ID
	_, err := Simulate(home, away, RawSeed(1, 1), nil)
	if err == nil || !strings.Contains(err.Error(), "appears in both clubs") {
		t.Fatalf("Simulate() error = %v, want shared-id error", err)
	}
}

func TestSimulateRejectsAnInvalidClub(t *testing.T) {
	home, away := averageClub("Home", "h"), averageClub("Away", "a")
	away.Name = ""
	if _, err := Simulate(home, away, RawSeed(1, 1), nil); err == nil {
		t.Fatal("Simulate() accepted a club with no name")
	}
}
