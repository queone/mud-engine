package proof

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GameAddress parses a game's address on a site, its box score page
// /games/{id} or its API answer /api/games/{id}, into the site's base address
// and the game id.
func GameAddress(raw string) (base string, id int64, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", 0, fmt.Errorf("proof: %q is not a game's address; give https://SITE/games/ID", raw)
	}
	path := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/proof")
	rest, ok := strings.CutPrefix(path, "/api/games/")
	if !ok {
		rest, ok = strings.CutPrefix(path, "/games/")
	}
	id, perr := strconv.ParseInt(rest, 10, 64)
	if !ok || perr != nil || id < 1 {
		return "", 0, fmt.Errorf("proof: %q is not a game's address; give https://SITE/games/ID or https://SITE/api/games/ID", raw)
	}
	return u.Scheme + "://" + u.Host, id, nil
}

// Fetch reads a game's proof, the commitment it played under, its own or its
// Day's as the proof names, and its box score from the site at a game's
// address.
func Fetch(ctx context.Context, client *http.Client, address string) (Proof, Commitment, BoxScore, error) {
	var (
		p   Proof
		c   Commitment
		box BoxScore
	)
	base, id, err := GameAddress(address)
	if err != nil {
		return p, c, box, err
	}
	if err := getJSON(ctx, client, fmt.Sprintf("%s/api/games/%d/proof", base, id), &p); err != nil {
		return p, c, box, err
	}
	commitment := fmt.Sprintf("%s/api/baseball-leagues/%d/seasons/%d/days/%d/commitment", base, p.BaseballLeague.ID, p.Season, p.Day)
	if scopeOf(p.CommitmentScope) == ScopeGame {
		commitment = fmt.Sprintf("%s/api/games/%d/commitment", base, id)
	}
	if err := getJSON(ctx, client, commitment, &c); err != nil {
		return p, c, box, err
	}
	if err := getJSON(ctx, client, fmt.Sprintf("%s/api/games/%d", base, id), &box); err != nil {
		return p, c, box, err
	}
	return p, c, box, nil
}

// getJSON decodes one JSON answer, naming the site's own error when the
// answer is not a success.
func getJSON(ctx context.Context, client *http.Client, address string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("proof: %s: %w", address, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("proof: fetching %s: %w; check the address and the network", address, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("proof: reading %s: %w", address, err)
	}
	if resp.StatusCode != http.StatusOK {
		var site struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &site) == nil && site.Error != "" {
			return fmt.Errorf("proof: %s answered %d: %s", address, resp.StatusCode, site.Error)
		}
		return fmt.Errorf("proof: %s answered %d", address, resp.StatusCode)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("proof: %s is not the expected JSON: %w", address, err)
	}
	return nil
}
