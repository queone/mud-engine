# mud-engine

The game engine Mud Baseball plays every game with, and `mud-verify`, which replays any of its games and checks the
game's proof. The site imports this module at a pinned version, so the code that plays games is this code, and Go's
public checksum database (sum.golang.org) records each version's exact content.

## Check a game

Pick a game on the site and run the newest verifier:

```sh
go run github.com/queone/mud-engine/cmd/mud-verify@latest https://mudball.one/games/1234
```

The newest verifier replays engine model 2, the model of every game before module v0.7.0, and model 3, which plays
each pitch through the count, whole from one round or live with a round for every pitch. A proof from a model it
does not replay names the verifier version that does, when it names one other than the verifier running.

It fetches the game's proof, the commitment it played under, and its box score, and checks seven links, eight for a game played pitch by pitch. A game
committed on its own, just before its scheduled start, has its own commitment at `/api/games/ID/commitment`; a game
played under its Day's commitment, as every game was before games started at their own times, has its Day's.


- engine: the proof names the engine model and the draw mode the game's commitment bound, so neither could be
  chosen once a round was known; a commitment from before model 3 binds model 2 and whole-game draws. A model this
  verifier does not replay names the version that does.
- commitment: the game's round and inputs hash are the ones its commitment bound, and a game's own commitment is for
  its slot and names its scheduled start.
- anchor: the commitment existed outside the site before its round: two RFC 3161 timestamp authorities, of
  Sigstore, DigiCert, and FreeTSA, signed its digest, the public Rekor v2 log holds an entry for it under the site's
  anchor key, and the round was published after the later timestamp. A game's own commitment binds exactly one
  round: the first published at or after its scheduled start and after the later timestamp, so the site cannot choose
  among later rounds; the anchor's statement names the start, so it was fixed before that round. A Day's commitment
  shows only that its round came after the later timestamp. A commitment without an anchor shows this link as `none`,
  and the verdict reads `verified, not anchored`; a game's own commitment without one still binds the first round at
  or after its start.
- inputs: the committed inputs hash to that commitment, and the game's inputs are a game's own commitment's inputs or
  its slot among its Day's.
- beacon: the drand quicknet round's signature verifies against the chain's public key, and the randomness is the
  SHA-256 of the signature.
- rounds, for a game played pitch by pitch: every pitch's round signature verifies, the first is the bound round,
  and the sequence follows the gap rule from it, 5 rounds between pitches with the bases empty, 7 with a runner on,
  and 40 after a half-inning or a mid-inning pitching change, so the site could not wait for a better round.
- seed: the seed derives from the baseball league's public seed, the season, the game number (Day*16+slot), and the
  round's randomness.
- replay: the model the commitment names replays the game from the inputs and the seed, model 3 pitch by pitch,
  and every inning, batter line, and pitcher line matches the box score. Each box score line names its player for the replay by `engine_player`, the player's id in
  the game's inputs; a box score saved before `engine_player` existed names it by a string `player` instead. A line
  whose `engine_player` is present but not a nonempty string, or that names no id at all, fails this link on that
  line, and the game is not verified.

It prints one line per link and exits 0 when all pass, 1 when one fails. `mud-verify -f PROOF -c COMMITMENT -b
BOXSCORE` checks saved copies of the three answers offline, including copies saved before `engine_player`.

## What it proves, and what it does not

- No one, the site included, could know the game before its drand round was published, barring collusion of a
  threshold of the League of Entropy's members (https://drand.love).
- The game's result follows from its committed inputs and that round, with the engine model its commitment names,
  and a game played pitch by pitch from the rounds the gap rule fixes after it.
- A changed result, inputs, or round no longer verifies: results are tamper-evident, not tamper-proof.
- An anchored commitment existed outside the site before its round, so even a game played late, after an outage
  or in a new baseball league's catch-up, was unknowable when committed.
- A game's own anchored commitment binds the one round its scheduled start and its anchor allow.
- The log makes a second commitment or anchor request for the same game or Day public, but finding one means
  reading the log for every entry under the site's key; the verifier checks only the entry the site serves.
- Rosters, lineups, and accounts are the site's; the proof covers how a game played from its inputs.

## Packages

- `engine`: one game's simulation from two clubs and a seed, pitch by pitch at model 3, played whole or a pitch at
  a time from a saved state; `engine/model2` is the frozen model every earlier game played under.
- `beacon`: drand quicknet constants, the pinned public key, round verification, and a relay client.
- `proof`: the input format, the inputs hash, the shapes the site serves, `Fetch`, `Check`, the anchor's
  statement and checks, and the pinned trust: the log's key and the timestamp authorities' roots.
- `cmd/mud-verify`: the verifier; [its page](cmd/mud-verify/README.md) holds its full help.

## Build

`./build.sh` checks a change: formatting, vet, staticcheck, the tests, and the verifier's version and help. It
then installs `mud-verify`.

## License

MIT; see `LICENSE`.
