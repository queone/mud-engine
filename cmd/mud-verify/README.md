# mud-verify

`mud-verify` replays a game Mud Baseball played and checks every link of its proof: the drand round's signature, the
seed, the game's inputs against the commitment it played under, its own or its Day's, and every line of the box
score. The [module README](../../README.md) explains each link and what a passing check proves.

Run the newest version, with nothing to install:

```sh
go run github.com/queone/mud-engine/cmd/mud-verify@latest https://mudball.one/games/1234
```

The newest version replays engine models 2 and 3, whole or pitch by pitch, and reads box scores in both shapes the
site has served, so it checks every game played so far, online or from saved copies.

## Help

### Usage

```text
mud-verify v0.7.1
Replay a Mud Baseball game and check every link of its proof
github.com/queone/mud-engine/cmd/mud-verify

Usage
  mud-verify URL
  mud-verify -f PROOF -c COMMITMENT -b BOXSCORE

  URL is a game's box score page, https://SITE/games/ID, or its API answer,
  https://SITE/api/games/ID. The second form checks saved copies of the game's
  /api/games/ID/proof, the commitment it played under, and /api/games/ID.

Options
  -f, --proof FILE        a saved proof
  -c, --commitment FILE   the saved commitment the game played under
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
