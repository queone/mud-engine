package engine

import (
	"errors"
	"fmt"
	"slices"
)

// Hand is a batting or throwing side.
type Hand string

// Batting and throwing sides. Switch applies to batters only.
const (
	Left   Hand = "L"
	Right  Hand = "R"
	Switch Hand = "S"
)

// Batting holds a batter's ratings from 1 to 99, where 50 is league average.
type Batting struct {
	Contact int `json:"contact"`
	Power   int `json:"power"`
	Eye     int `json:"eye"`
	Speed   int `json:"speed"`
	Defense int `json:"defense"`
}

// Pitching holds a pitcher's ratings from 1 to 99, where 50 is league average.
type Pitching struct {
	Stuff    int `json:"stuff"`
	Control  int `json:"control"`
	Movement int `json:"movement"`
	Stamina  int `json:"stamina"`
}

// Player is one person with an identity and the ratings for the roles they fill.
type Player struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Bats      Hand      `json:"bats"`
	Throws    Hand      `json:"throws"`
	Positions []string  `json:"positions,omitempty"`
	Batting   *Batting  `json:"batting,omitempty"`
	Pitching  *Pitching `json:"pitching,omitempty"`
}

// Slot is one lineup place: the player and the position they field, or DH.
type Slot struct {
	Player   Player `json:"player"`
	Position string `json:"position"`
}

// Bullpen is the relief corps in usage order: closer, setup man, then the rest.
type Bullpen struct {
	Closer Player   `json:"closer"`
	Setup  Player   `json:"setup"`
	Others []Player `json:"others"`
}

// Club is one side of a game: a nine-slot lineup, the starting pitcher, and the bullpen.
type Club struct {
	Name    string  `json:"name"`
	Lineup  [9]Slot `json:"lineup"`
	Starter Player  `json:"starter"`
	Bullpen Bullpen `json:"bullpen"`
}

// Positions a lineup slot may hold. DH bats without fielding.
var positions = []string{"C", "1B", "2B", "3B", "SS", "LF", "CF", "RF", "DH"}

// MinRelievers is the smallest bullpen Validate accepts.
const MinRelievers = 3

// Validate reports the first way the club cannot take the field, naming the
// club, the slot or player, and the field. A player may appear once in the
// lineup and once on the pitching staff.
func (c Club) Validate() error {
	if c.Name == "" {
		return errors.New("engine: club has no name")
	}
	ids := map[string]bool{}
	pitcherIDs := map[string]bool{}
	dh, catcher := 0, 0
	for i, s := range c.Lineup {
		where := fmt.Sprintf("lineup slot %d", i+1)
		if err := c.checkIdentity(s.Player, where, ids); err != nil {
			return err
		}
		if !slices.Contains(positions, s.Position) {
			return fmt.Errorf("engine: club %q: %s: position %q is not one of %v", c.Name, where, s.Position, positions)
		}
		switch s.Position {
		case "DH":
			dh++
		case "C":
			catcher++
		}
		if s.Player.Batting == nil {
			return fmt.Errorf("engine: club %q: %s: player %q has no batting ratings", c.Name, where, s.Player.ID)
		}
		if err := checkHand(c.Name, where, "bats", s.Player.Bats, true); err != nil {
			return err
		}
		b := s.Player.Batting
		ratings := []struct {
			name  string
			value int
		}{{"Contact", b.Contact}, {"Power", b.Power}, {"Eye", b.Eye}, {"Speed", b.Speed}, {"Defense", b.Defense}}
		for _, r := range ratings {
			if err := checkRating(c.Name, where, r.name, r.value); err != nil {
				return err
			}
		}
	}
	if dh != 1 {
		return fmt.Errorf("engine: club %q: lineup has %d DH slots; need exactly 1", c.Name, dh)
	}
	if catcher != 1 {
		return fmt.Errorf("engine: club %q: lineup has %d C slots; need exactly 1", c.Name, catcher)
	}
	if err := c.checkPitcher(c.Starter, "starter", pitcherIDs); err != nil {
		return err
	}
	relievers := c.relievers()
	if len(relievers) < MinRelievers {
		return fmt.Errorf("engine: club %q: bullpen has %d relievers; need at least %d", c.Name, len(relievers), MinRelievers)
	}
	for i, p := range relievers {
		if err := c.checkPitcher(p, fmt.Sprintf("reliever %d", i+1), pitcherIDs); err != nil {
			return err
		}
	}
	return nil
}

// relievers lists the bullpen in usage order: closer, setup man, then the rest.
func (c Club) relievers() []Player {
	out := make([]Player, 0, 2+len(c.Bullpen.Others))
	out = append(out, c.Bullpen.Closer, c.Bullpen.Setup)
	return append(out, c.Bullpen.Others...)
}

// players lists every player on the club.
// Players lists every player of the club: the lineup, the starter, and the
// bullpen.
func (c Club) Players() []Player { return c.players() }

func (c Club) players() []Player {
	out := make([]Player, 0, 10+len(c.Bullpen.Others))
	for _, s := range c.Lineup {
		out = append(out, s.Player)
	}
	out = append(out, c.Starter)
	return append(out, c.relievers()...)
}

func (c Club) checkIdentity(p Player, where string, ids map[string]bool) error {
	if p.ID == "" {
		return fmt.Errorf("engine: club %q: %s: player has no id", c.Name, where)
	}
	if ids[p.ID] {
		return fmt.Errorf("engine: club %q: %s: player id %q is used twice", c.Name, where, p.ID)
	}
	ids[p.ID] = true
	return nil
}

func (c Club) checkPitcher(p Player, where string, ids map[string]bool) error {
	if err := c.checkIdentity(p, where, ids); err != nil {
		return err
	}
	if p.Pitching == nil {
		return fmt.Errorf("engine: club %q: %s: player %q has no pitching ratings", c.Name, where, p.ID)
	}
	if err := checkHand(c.Name, where, "throws", p.Throws, false); err != nil {
		return err
	}
	r := p.Pitching
	ratings := []struct {
		name  string
		value int
	}{{"Stuff", r.Stuff}, {"Control", r.Control}, {"Movement", r.Movement}, {"Stamina", r.Stamina}}
	for _, x := range ratings {
		if err := checkRating(c.Name, where, x.name, x.value); err != nil {
			return err
		}
	}
	return nil
}

func checkHand(club, where, field string, h Hand, allowSwitch bool) error {
	switch h {
	case Left, Right:
		return nil
	case Switch:
		if allowSwitch {
			return nil
		}
	}
	allowed := "L or R"
	if allowSwitch {
		allowed = "L, R, or S"
	}
	return fmt.Errorf("engine: club %q: %s: %s %q is not %s", club, where, field, string(h), allowed)
}

func checkRating(club, where, field string, v int) error {
	if v < 1 || v > 99 {
		return fmt.Errorf("engine: club %q: %s: %s %d is out of range 1..99", club, where, field, v)
	}
	return nil
}
