# entra-helper

A small single-binary CLI that authenticates against **Microsoft Entra ID** and prints an
OAuth2 **access token** to stdout. Useful for scripting, local development, and feeding tokens
into `curl`, REST clients, or other tools.

It supports three credential flows and persists tokens between runs so you only sign in once.

## Features

- Three authentication flows as subcommands:
  - `interactive` — interactive browser sign-in
  - `devicecode` — device code flow (good for headless / remote shells)
  - `default` — `DefaultAzureCredential` (environment, managed identity, Azure CLI, ...)
- **Token persistence**: tokens are cached in the OS keychain (Keychain on macOS) and an
  authentication record is stored on disk, so subsequent runs refresh silently without prompting.
- Token goes to **stdout**, prompts/diagnostics go to **stderr** — safe to use in pipelines.

## Requirements

Toolchain is managed by [mise](https://mise.jdx.dev/). The Go-specific tools live in
`mise.golang.toml`, which is only active when `MISE_ENV` includes `golang`.

```bash
export MISE_ENV=base,golang   # or set it in your shell / direnv
mise install                  # installs Go, golangci-lint, govulncheck, ...
```

## Build

```bash
go build -o entra-helper .
```

## Usage

```text
entra-helper <command> --scope <scope> [flags]
```

`--scope` is **required** (repeatable or comma-separated).

### Examples

```bash
# Device code flow (prints a code + URL to stderr)
./entra-helper devicecode \
  --tenant-id <tenant-id> --client-id <client-id> \
  --scope https://graph.microsoft.com/.default

# Interactive browser flow
./entra-helper interactive \
  --tenant-id <tenant-id> --client-id <client-id> \
  --scope api://my-api/.default

# DefaultAzureCredential (env vars, managed identity, az cli, ...)
TOKEN=$(./entra-helper default --scope https://graph.microsoft.com/.default)
curl -H "Authorization: Bearer $TOKEN" https://graph.microsoft.com/v1.0/me
```

### Flags

| Flag           | Description                                              | Default                                    |
| -------------- | -------------------------------------------------------- | ------------------------------------------ |
| `--scope`      | Token scope, **required**; repeatable or comma-separated | —                                          |
| `--tenant-id`  | Entra tenant ID                                          | `$AZURE_TENANT_ID`                         |
| `--client-id`  | Application (client) ID                                  | `$AZURE_CLIENT_ID`                         |
| `--expires-on` | Also print the token expiry to stderr                    | `false`                                    |
| `--no-cache`   | Disable the persistent token cache                       | `false`                                    |
| `--record`     | Path to persist the authentication record                | `$XDG_CACHE/entra-helper/auth-record.json` |

## Token persistence

For the `interactive` and `devicecode` flows:

1. **First run** — you sign in normally. The access/refresh tokens are stored in the OS keychain,
   and an `AuthenticationRecord` is written to `--record` (mode `0600`).
2. **Later runs** — the record + cache are loaded and the token is refreshed silently, with no
   prompt.

Notes:

- `default` does not use the persistent record (the underlying credentials manage their own state).
- Disable persistence with `--no-cache`.
- If you switch tenants/clients, point `--record` at a different file to keep records separate.

## Use as an API key / token helper

`entra-helper` can feed Entra bearer tokens to AI coding tools (Claude Code, Codex, Copilot)
that talk to Azure OpenAI or an Entra-protected gateway. See
[docs/api-key-helper.md](docs/api-key-helper.md) and the wrapper in
[examples/entra-token.sh](examples/entra-token.sh).

## Releases

Pushing a `v*` tag triggers [`.github/workflows/release.yml`](.github/workflows/release.yml),
which runs [GoReleaser](https://goreleaser.com/) to build cross-platform archives and publish a
GitHub Release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release runs on a macOS runner so the darwin build can use cgo (required by the
keychain-backed token cache); linux/windows are cross-compiled with cgo disabled. Validate the
config and build a local snapshot without publishing:

```bash
mise run release:check
mise run release:snapshot   # artifacts land in dist/
```

## Development

```bash
mise run fmt     # format
mise run lint    # go vet + golangci-lint + govulncheck
mise run test    # go test ./...
mise run ci      # lint + test
```
