# AGENTS.md

This repository is a child of a private parent repository, the Mud Baseball site, which plans, validates, and
releases it.

- Make changes only from the parent repository's sessions, under the parent's governance.
- Check every change with `gofmt -l .`, `go vet ./...`, `staticcheck ./...`, and `go test ./...`.
- Keep engine behavior byte-for-byte deterministic: a change to rates, tables, or rules bumps `engine.Version`.
- Write commit messages and `CHANGELOG.md` rows in plain words, without the parent's AC identifiers.
- Keep everything here readable by outsiders: no private paths, names, or decisions from the parent.
