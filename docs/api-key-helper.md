# Using entra-helper as an API key / token helper

`entra-helper` prints a fresh Microsoft Entra ID access token to **stdout** (and nothing
else — prompts and diagnostics go to stderr). That makes it a drop-in **bearer-token source**
for AI coding tools that talk to an Entra-protected endpoint, such as **Azure OpenAI** or a
corporate LLM **gateway** that authenticates callers with Entra ID.

The typical setup is:

```
tool  ──Authorization: Bearer <token>──▶  Entra-protected endpoint (Azure OpenAI / gateway)
  ▲
  └── token comes from `entra-helper`
```

## Key requirements for a "helper"

A key helper is invoked **non-interactively** and **repeatedly**, so:

1. **It must not prompt.** Prefer the `default` flow, backed by `az login` (developer machine)
   or a managed identity (CI / servers):

   ```bash
   az login                                   # one time
   entra-helper default --scope <scope>    # silent from then on
   ```

   The `interactive` / `devicecode` flows also work _after a one-time login_, because the token
   is cached (see [token persistence](../README.md#token-persistence)) and refreshed silently —
   but if the refresh token ever expires they would need to prompt again, which fails inside a
   helper. `default` + `az login` is the most robust choice.

2. **It must print only the token.** `entra-helper` already does this — do **not** pass
   `--expires-on` in a helper (that prints to stderr anyway, but keep it clean).

3. **Pick the right `--scope`** for your endpoint:
   - Azure OpenAI / Azure AI services: `https://cognitiveservices.azure.com/.default`
   - A custom app-registration gateway: `api://<app-id>/.default`

A ready-made wrapper is provided at [`examples/entra-token.sh`](../examples/entra-token.sh).

---

## Claude Code

Claude Code natively supports a key helper via the **`apiKeyHelper`** setting: a command whose
stdout is used as the credential (sent as both `Authorization: Bearer` and `X-Api-Key`). This is
the cleanest integration of the three.

1. Make the wrapper executable and point it at your scope:

   ```bash
   install -m 0755 examples/entra-token.sh /usr/local/bin/entra-token
   ```

2. Configure `~/.claude/settings.json` to use your Entra-protected endpoint and the helper:

   ```json
   {
     "env": {
       "ANTHROPIC_BASE_URL": "https://your-llm-gateway.example.com"
     },
     "apiKeyHelper": "/usr/local/bin/entra-token"
   }
   ```

3. Tell Claude Code how often to refresh the token. Entra access tokens last ~60–90 min; refresh
   comfortably inside that window (e.g. every 50 min):

   ```bash
   export CLAUDE_CODE_API_KEY_HELPER_TTL_MS=3000000
   ```

Claude Code re-runs the helper on that interval, so tokens rotate without manual steps.

> Note: this assumes `ANTHROPIC_BASE_URL` points at a gateway that accepts Entra bearer tokens
> and speaks the Anthropic API. Use the scope that gateway expects.

---

## Codex CLI

Codex has a native **command-backed bearer token** mechanism — the closest equivalent to Claude
Code's `apiKeyHelper`. Under `[model_providers.<id>.auth]` you give it a `command`; Codex runs it
when it needs a token, trims stdout, and sends the result as the `Authorization: Bearer` header.
It re-runs the command on `refresh_interval_ms`, so tokens rotate automatically — no wrapper
script and no env var needed.

`~/.codex/config.toml`:

```toml
model = "gpt-5.4"
model_provider = "azure"

[model_providers.azure]
name = "Azure OpenAI"
base_url = "https://YOUR-RESOURCE.openai.azure.com/openai"

[model_providers.azure.auth]
command = "entra-helper"
args = ["default", "--scope", "https://cognitiveservices.azure.com/.default"]
timeout_ms = 10000 # default 5000; raise if `default` may hit a slow `az` path
refresh_interval_ms = 1800000 # default 300000 (5 min); Entra tokens last ~1h
```

The `auth` command contract: it receives no stdin, writes only the token to stdout (trimmed), and
exits 0. `entra-helper` already satisfies this. `command` must be on `PATH` or an absolute path.

Notes:

- This sends the token as a **bearer** token, which is what Azure OpenAI expects for Entra
  (Microsoft Entra ID) authentication — make sure your deployment is configured for Entra auth,
  not just `api-key`.
- It must stay non-interactive, so use `default` (after `az login` / managed identity). See the
  [helper requirements](#key-requirements-for-a-helper) above.
- Older Codex versions without `[...auth]` only support `env_key` (a static env var). On those,
  fall back to exporting the token before launch:
  ```bash
  export AZURE_OPENAI_TOKEN="$(entra-helper default \
    --scope https://cognitiveservices.azure.com/.default)"
  codex   # provider configured with env_key = "AZURE_OPENAI_TOKEN"
  ```

---

## GitHub Copilot

Copilot's own models authenticate with your GitHub account. Its **BYOK** ("bring your own key",
GA in 2026 for Copilot CLI and VS Code) lets you point Copilot at Azure OpenAI or any
OpenAI-compatible endpoint — but **only with a static key**. There is **no command/key-helper hook
and no token refresh**: the Copilot SDK's `bearerToken` is a static string, and if it expires
requests fail until you start a new session. So Copilot is the weakest fit for ~1h Entra tokens.

Copilot CLI BYOK is configured via environment variables:

| Variable                    | Purpose                            |
| --------------------------- | ---------------------------------- |
| `COPILOT_PROVIDER_BASE_URL` | endpoint URL (required)            |
| `COPILOT_PROVIDER_TYPE`     | `openai` \| `azure` \| `anthropic` |
| `COPILOT_PROVIDER_API_KEY`  | the key / bearer token             |
| `COPILOT_MODEL`             | model identifier (required)        |

Two ways to use `entra-helper` despite the static-key limitation:

1. **Export-then-launch (token good for ~1h)** — inject a fresh token at startup; restart the
   session when it expires:
   ```bash
   export COPILOT_PROVIDER_BASE_URL="https://YOUR-RESOURCE.openai.azure.com"
   export COPILOT_PROVIDER_TYPE="azure"
   export COPILOT_MODEL="gpt-5.4"
   export COPILOT_PROVIDER_API_KEY="$(entra-helper default \
     --scope https://cognitiveservices.azure.com/.default)"
   copilot
   ```

2. **Gateway (recommended for long sessions)** — front Azure OpenAI with a gateway that performs
   the Entra exchange itself, and give Copilot a stable gateway key. Here `entra-helper` runs
   _inside the gateway_, so Copilot never sees an expiring token.

Unlike Claude Code (`apiKeyHelper`) and Codex (`[...auth].command`), Copilot has no refreshing
helper today — prefer option 2 if you hit mid-session expiries.

---

## Troubleshooting

- **Helper prompts / hangs**: you used `interactive`/`devicecode` without a prior login. Run the
  login once, or switch to `default` after `az login`.
- **401 from the endpoint**: wrong `--scope`, or the endpoint isn't configured for Entra
  bearer auth. Verify with:
  ```bash
  TOKEN=$(entra-helper default --scope <scope>)
  curl -sS -H "Authorization: Bearer $TOKEN" <endpoint>/health
  ```
- **Token expired mid-session**: lower the tool's refresh interval (e.g.
  `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`) or restart env-injection wrappers more often.
