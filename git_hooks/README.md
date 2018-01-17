# Couchbase Operator Git Hooks

Seemlessly handles updating dependencies when switching between commits and
branches and when handling rebases, thus avoiding puzzling and random
compilation errors.

## Installation

    cp git_hooks/* .git/hooks

## Caveats

Running `glide update` may end up generating a different `glide.lock` file
than what is currently checked in to the repository, so be careful when
committing.
