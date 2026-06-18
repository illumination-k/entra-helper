#!/usr/bin/env bash
# Non-interactive token helper: prints ONLY a fresh Entra access token to stdout.
#
# Intended to be wired into tools that accept a "key helper" command
# (e.g. Claude Code's apiKeyHelper). It must never prompt, so it uses the
# `default` flow (Azure CLI / managed identity / env vars). Run an interactive
# login once beforehand (e.g. `az login`) so this can refresh silently.
set -euo pipefail

# The scope your gateway / Azure resource expects. Override via ENTRA_SCOPE.
#   Azure OpenAI / Cognitive Services: https://cognitiveservices.azure.com/.default
#   Custom app gateway:                api://<app-id>/.default
SCOPE="${ENTRA_SCOPE:-https://cognitiveservices.azure.com/.default}"

exec entra-helper default --scope "$SCOPE"
