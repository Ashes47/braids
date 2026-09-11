# braids

Search every Claude Code conversation on this machine, read the turns around
what you find, and branch a new conversation from that exact point.

```
/plugin marketplace add Ashes47/braids
/plugin install braids
```

## It needs the binary

This plugin is the skill and the hooks. The thing that reads your transcripts
is a single binary, and the plugin cannot install it for you:

```sh
curl -fsSL https://braids.chat/install.sh | sh
```

Without it the skill still loads, and the first thing it tells Claude to do is
check whether braids is installed, so you get told rather than left guessing.
The hooks call `braids hook`, which does nothing at all when there is no
braids on PATH, and a hook that does nothing does not disturb a session.

## What you get

The **skill** teaches Claude when to reach for your history and, at more
length, when not to: the question in front of it is usually answered by the
code, and searching everything on every question is slow and beside the point.
It covers searching, reading the turns around a hit, joining a branch back,
and what braids can and cannot honestly claim.

The **hooks** let braids tell a session that stopped and asked you something
apart from one that finished. Nothing else on the machine can see that
difference, and without it every conversation looks the same in the map.

Nothing here talks to a model or makes a network call. Everything it reports
is read out of files Claude Code already wrote.

## Then

```sh
braids doctor      # whether braids can be believed
braids brief       # what has been going on in this project
braids             # the map
```

Documentation is at [braids.chat](https://braids.chat).
