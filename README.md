# rline

`rline` is a readline package for Go. A readline package reads a line of text
from a terminal, and gives the user editing, history, and completion.

This package is written in pure Go. It does not use cgo, and it does not link
against the GNU readline C library.

## Status

The package is new. It contains no code yet.

## Goals

`rline` aims to match the features of the GNU readline library:

1. Read `inputrc` files. `inputrc` is the configuration file format that GNU
   readline reads.
2. Run on Windows as well as on Unix systems.
3. Complete words when the user presses Tab.

`rline` also aims to add two features that GNU readline does not have. It
highlights syntax as the user types, and it shows completions in a menu.

## About

`rline` supports [usql](https://github.com/xo/usql), a universal command-line
interface for SQL databases.
