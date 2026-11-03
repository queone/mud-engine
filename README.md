# mud-engine

The game engine Mud Baseball plays every game with, and `mud-verify`, which replays any of its games and checks the
game's proof. The site imports this module at a pinned version, so the code that plays games is this code, and Go's
public checksum database (sum.golang.org) records each version's exact content.

## Check a game

Pick a game on the site and run the verifier at the version its proof names:

```sh
go run github.com/queone/mud-engine/cmd/mud-verify@v0.1.0 https://mudball.one/games/1234
```

It fetches the game's proof, its Day's commitment, and its box score, and checks six links:

- engine: the game used this verifier's engine model; a game from another model names the version that replays it.
- commitment: the game's round and inputs hash are the ones its Day committed to.
- inputs: the Day's inputs hash to that commitment, and the game's inputs are its slot among them.
- beacon: the drand quicknet round's signature verifies against the chain's public key, and the randomness is the
  SHA-256 of the signature.
- seed: the seed derives from the circuit's public seed, the season, the game number (Day*16+slot), and the
  round's randomness.
- replay: the engine replays the game from the inputs and the seed, and every inning, batter line, and pitcher line
  matches the box score.

It prints one line per link and exits 0 when all pass, 1 when one fails. `mud-verify -f PROOF -c COMMITMENT -b
BOXSCORE` checks saved copies of the three answers offline.

## What it proves, and what it does not

- No one, the site included, could know the game before its drand round was published, barring collusion of a
  threshold of the League of Entropy's members (https://drand.love).
- The game's result follows from its committed inputs and that round, with this engine.
- A changed result, inputs, or round no longer verifies: results are tamper-evident, not tamper-proof.
- The commitment is stored by the site; anchoring it outside the site is still to come.
- Rosters, lineups, and accounts are the site's; the proof covers how a game played from its inputs.

## Packages

- `engine`: one game's simulation from two clubs and a seed.
- `beacon`: drand quicknet constants, the pinned public key, round verification, and a relay client.
- `proof`: the input format, the inputs hash, the shapes the site serves, `Fetch`, and `Check`.
- `cmd/mud-verify`: the verifier.

## License

MIT; see `LICENSE`.
