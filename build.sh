#!/usr/bin/env bash
# build.sh — build UBR for all target platforms
# Usage: ./build.sh [--version VERSION]
set -euo pipefail

# ── version ──────────────────────────────────────────────────────────────────
BASE_VERSION=$(tr -d '[:space:]' < VERSION)
GIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "nogit")
VERSION="${BASE_VERSION}-${GIT_HASH}"

# Allow caller to override (used by CI)
if [[ "${1:-}" == "--version" && -n "${2:-}" ]]; then
    VERSION="$2"
fi

LDFLAGS="-s -w -X main.version=${VERSION}"
OUTDIR="dist"

# ── targets ──────────────────────────────────────────────────────────────────
# Format: GOOS GOARCH GOARM(optional)
# Note: raw socket (rebroadcast) and AF_PACKET (monitor) are Linux-only.
#       Windows/macOS builds compile but those features are gracefully disabled.
declare -a TARGETS=(
    "linux   amd64  "
    "linux   arm64  "
    "linux   arm    7"
    "windows amd64  "
    "darwin  amd64  "
    "darwin  arm64  "
)

# ── build ─────────────────────────────────────────────────────────────────────
echo "Building UBR ${VERSION}"
echo "Output: ${OUTDIR}/"
echo
mkdir -p "${OUTDIR}"

build() {
    local goos="$1" goarch="$2" goarm="${3:-}"
    local name="ubr-${goos}-${goarch}"
    [[ "$goos" == "windows" ]] && name="${name}.exe"
    local out="${OUTDIR}/${name}"
    printf "  %-20s ->  %s\n" "${goos}/${goarch}" "${out}"
    GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
        go build -trimpath -ldflags="$LDFLAGS" -o "$out" ./cmd/ubr
}

for entry in "${TARGETS[@]}"; do
    read -r goos goarch goarm <<< "$entry"
    build "$goos" "$goarch" "$goarm"
done

# ── checksums ────────────────────────────────────────────────────────────────
echo
echo "Generating checksums..."
(cd "${OUTDIR}" && sha256sum ubr-* > SHA256SUMS)
echo "  ${OUTDIR}/SHA256SUMS"

echo
echo "Done."
ls -lh "${OUTDIR}"/
