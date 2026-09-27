# Contributing to rline

rline is a pure Go port of isocline, and [usql][usql] is the program it
exists for. This file is for a person who changes the project.
[AGENTS.md](AGENTS.md) holds the full rules for a coding agent, and most of
them hold for a person too.

## What a change must do

The port keeps the behavior of the C, even where the C is wrong, because the
output recorded from the C is the test corpus. If a change makes rline do
something different from isocline, it needs a comment where the code does
it, an entry in "Known departures from the C code" in
[docs/PORT.md](docs/PORT.md), and a decision in
[docs/PLAN.md](docs/PLAN.md).

Something that isocline does not have, such as `Password`, has no corpus to
compare against. Test it at the layers that [docs/TESTING.md](docs/TESTING.md)
names, and never widen a C corpus to cover it.

## What you need

- Go 1.25.0 or later. That is enough to build, test and lint.
- A C compiler and a checkout of isocline in `isocline/`, but only to
  regenerate a corpus from the C. The checkout is a separate git repository,
  and git ignores it here. Never write inside it:

  ```sh
  git clone https://github.com/daanx/isocline.git isocline
  ```

- On Windows, a C compiler for `go test -race`, because the race detector
  needs cgo there. Nothing else needs one.

## Before you open a pull request

Run these in the repository root. Each must pass, and `gofmt -l .` must
print nothing:

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race . ./key
tools/lint.sh
```

On Windows, run `go test -skip TestGoldenSetIsPresent ./...` and
`pwsh -File tools/lint.ps1` instead. Nobody has recorded a session on
Windows yet, so that one test fails there.

If you renamed anything or changed a file with a build tag, also run the
loop in `.github/workflows/test.yml` that runs `go vet` for every system. A
file tagged for another system is invisible to a build on your own.

## Corpora and recorded sessions

A corpus in `testdata/` is made by a C probe in `tools/`, and it is never
edited by hand. To make it again, run `go test -update` in the package that
reads it. `go test ./internal/capture -update` records the sessions, on
Linux or macOS.

Before you trust a new test, break the code on purpose with
`tools/mutate.sh` and make sure that the test fails.

## Writing

Write documents, code comments, error messages and commit messages in plain
English. Use short sentences and the active voice. Do not use contractions,
semicolons or em dashes. Use "must" and "can", not "should" and "may". The
`simple-english` skill in `.agents/skills` holds the full rules.

Every document except `README.md`, `AGENTS.md`, `CLAUDE.md` and this file
goes in `docs/`.

## Agent skills

The repository carries two agent skills. A skill is a set of instructions
that a coding agent loads for a task. `simple-english` sets how prose is
written, and `go-pedantry` sets how Go is written.

`skills-lock.json` names the source of each skill. The `skills` command from
npm writes that file. It writes each skill into two folders. Codex and other
agents read `.agents/skills/<name>`, and Claude Code reads
`.claude/skills/<name>`.

To install the skills again, or to update them, run these two commands in
the repository root:

```sh
npx skills@1.7.0 add AminBlg/SimpleEnglish --skill simple-english --agent codex claude-code --copy -y
npx skills@1.7.0 add oborchers/fractional-cto --skill go-pedantry --agent codex claude-code --copy -y
```

Keep `--copy`. Without it, the command writes `.claude/skills/<name>` as a
symbolic link. A Windows checkout writes a symbolic link as a small text
file, and Claude Code then loads no skill and says nothing.
`TestSkillsAreCopies` fails on a link, on a missing copy, on two copies that
differ, and on a skill folder that `skills-lock.json` does not name.

[usql]: https://github.com/xo/usql
