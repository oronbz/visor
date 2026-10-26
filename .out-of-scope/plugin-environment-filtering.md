# Filtering the environment Plugins start with

visor starts every Plugin process with its own whole environment, and doesn't
filter it down to a fixed set of variables plus the ones a Plugin's manifest
names.

## Why this is out of scope

A Plugin isn't sandboxed. The trust prompt says it plainly: "A Plugin runs with
your permissions." A Plugin that wants a credential exported in the shell can
just as well read `~/.config/gh/hosts.yml`, `~/.aws/credentials`, `~/.netrc` or
the shell's rc files. Filtering the environment closes one of those paths and
leaves the rest open, so it would look like a security boundary without being
one. Trust is the boundary: install only Plugins you'd let run as you.

Filtering would also cost more than it buys:

- It breaks Plugins that read a variable today. The official Bitrise Plugin
  reads `BITRISE_TOKEN` from the environment when there's no keyring.
- A new `env` key in the manifest fails to load on an older visor, because the
  manifest schema rejects keys it doesn't know.
- The variables every Plugin needs differ by OS and setup: locale, `TZ`,
  Windows' `SystemRoot` and `APPDATA`, and the proxy and CA variables that
  corporate networks need. Getting one of them wrong breaks a Plugin in ways
  that are hard to diagnose.

Secret settings remain the way to give a Plugin a credential that only that
Plugin holds.

## Prior requests

- #65: "Plugins inherit visor's whole environment, credentials included"
