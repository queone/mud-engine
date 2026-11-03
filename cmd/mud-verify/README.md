# mud-verify

`mud-verify` replays a game Mud Baseball played and checks every link of its proof: the drand round's signature, the
seed, the Day's inputs against their commitment, and every line of the box score. The [module README](../../README.md)
explains each link and what a passing check proves.

Run it at the version a game's proof names, with nothing to install:

```sh
go run github.com/queone/mud-engine/cmd/mud-verify@v0.3.0 https://mudball.one/games/1234
```

## Help

### Usage

```text
mud-verify v0.3.0
Replay a Mud Baseball game and check every link of its proof
github.com/queone/mud-engine/cmd/mud-verify

Usage
  mud-verify URL
  mud-verify -f PROOF -c COMMITMENT -b BOXSCORE

  URL is a game's box score page, https://SITE/games/ID, or its API answer,
  https://SITE/api/games/ID. The second form checks saved copies of the game's
  /api/games/ID/proof, its Day's commitment, and /api/games/ID.

Options
  -f, --proof FILE        a saved proof
  -c, --commitment FILE   the saved commitment of the game's Day
  -b, --box FILE          the saved box score
  -v, --version           print the version
  -h, -?, --help          show this help

Exit
  0  every link passed
  1  a link failed, or the proof could not be read
  2  the command line is wrong

Examples
  mud-verify https://mudball.one/games/1234
  mud-verify -f proof.json -c commitment.json -b box.json
```
