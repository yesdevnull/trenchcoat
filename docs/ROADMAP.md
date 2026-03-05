# Trenchcoat Roadmap

The following features are out of scope for the initial build but should be considered in the architecture to avoid painful refactors later.

## Passthrough Mode

A hybrid of serve and proxy. Serve matched coats as mocks, but forward unmatched requests to a real upstream URL. This will likely become a `--passthrough <upstream-url>` flag on the `serve` subcommand.

## Complex Directory Structure

Support recursive directory loading with organisational conventions (e.g. `mocks/users/list.yaml`, `mocks/auth/login.yaml`) and potential shared default headers/config at directory level.

## ~~Request Body Matching~~ (Implemented)

~~Allow coats to match on request body content for POST/PUT/PATCH requests.~~ Implemented as exact string matching via the `request.body` field (`*string` — `nil` means match any body, set value means exact match). Proxy capture includes `--capture-body` flag (default: `true`). Future enhancements could add substring, glob, or JSONPath matching modes.
