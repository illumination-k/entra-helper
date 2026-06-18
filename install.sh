#!/usr/bin/env bash
# Install the entra-helper binary from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/illumination-k/entra-helper/main/install.sh | bash
#
# Environment overrides:
#   VERSION       release tag to install (e.g. v0.1.0); defaults to the latest release
#   INSTALL_DIR   install destination; defaults to /usr/local/bin (falls back to ~/.local/bin)
set -euo pipefail

REPO="illumination-k/entra-helper"
BINARY="entra-helper"
VERSION="${VERSION:-latest}"

info() { printf '\033[32m==>\033[0m %s\n' "$*" >&2; }
err() {
	printf '\033[31merror:\033[0m %s\n' "$*" >&2
	exit 1
}

need() { command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"; }

detect_os() {
	case "$(uname -s)" in
	Linux) echo "linux" ;;
	Darwin) echo "darwin" ;;
	*) err "unsupported OS: $(uname -s) (use the Windows archive from the Releases page)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo "amd64" ;;
	aarch64 | arm64) echo "arm64" ;;
	*) err "unsupported architecture: $(uname -m)" ;;
	esac
}

# Resolve "latest" to a concrete tag via the releases/latest redirect (no API token, no jq).
resolve_tag() {
	if [ "$VERSION" != "latest" ]; then
		echo "$VERSION"
		return
	fi
	local url
	url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
	case "$url" in
	*/tag/*) echo "${url##*/tag/}" ;;
	*) err "could not determine the latest release tag" ;;
	esac
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

main() {
	need curl
	need tar

	local os arch tag version archive base_url tmp
	os="$(detect_os)"
	arch="$(detect_arch)"
	tag="$(resolve_tag)"
	version="${tag#v}"
	archive="${BINARY}_${version}_${os}_${arch}.tar.gz"
	base_url="https://github.com/${REPO}/releases/download/${tag}"

	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT

	info "Downloading ${archive} (${tag})"
	curl -fsSL -o "${tmp}/${archive}" "${base_url}/${archive}" ||
		err "download failed: ${base_url}/${archive}"

	# Verify the checksum against the published checksums.txt.
	if curl -fsSL -o "${tmp}/checksums.txt" "${base_url}/checksums.txt" 2>/dev/null; then
		local want got
		want="$(awk -v f="$archive" '$2 == f {print $1}' "${tmp}/checksums.txt")"
		got="$(sha256_of "${tmp}/${archive}")"
		[ -n "$want" ] || err "no checksum entry for ${archive}"
		[ "$want" = "$got" ] || err "checksum mismatch for ${archive} (want ${want}, got ${got})"
		info "Checksum verified"
	else
		info "checksums.txt not found; skipping verification"
	fi

	tar -xzf "${tmp}/${archive}" -C "$tmp"
	[ -f "${tmp}/${BINARY}" ] || err "binary ${BINARY} not found in archive"
	chmod +x "${tmp}/${BINARY}"

	# Choose an install directory, preferring a writable one without sudo.
	local dir="${INSTALL_DIR:-/usr/local/bin}"
	if [ ! -d "$dir" ] || [ ! -w "$dir" ]; then
		if [ -z "${INSTALL_DIR:-}" ] && command -v sudo >/dev/null 2>&1 && [ -d "$dir" ]; then
			info "Installing to ${dir} (requires sudo)"
			sudo install -m 0755 "${tmp}/${BINARY}" "${dir}/${BINARY}"
			info "Installed ${BINARY} ${tag} -> ${dir}/${BINARY}"
			return
		fi
		dir="${HOME}/.local/bin"
		mkdir -p "$dir"
	fi

	install -m 0755 "${tmp}/${BINARY}" "${dir}/${BINARY}"
	info "Installed ${BINARY} ${tag} -> ${dir}/${BINARY}"
	case ":${PATH}:" in
	*":${dir}:"*) ;;
	*) info "note: ${dir} is not on your PATH; add it to use ${BINARY}" ;;
	esac
}

main "$@"
