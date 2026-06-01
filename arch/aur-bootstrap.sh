#!/usr/bin/env bash
# One-shot bootstrap of the AUR `fsvc-bin` package repo.
#
# AUR creates the git repo on first SSH push, but only if the push has
# at least one commit. Goreleaser's `aurs:` block (and aur-bump.sh) rely
# on git-clone working, which requires the repo to already exist AND to
# contain a HEAD ref. So somebody has to push the very first PKGBUILD +
# .SRCINFO. This script does that — exactly once, then never again
# (subsequent updates flow through goreleaser on every git tag).
#
# Idempotent against re-runs: if the AUR repo is already non-empty,
# this prints a hint and exits 0.
#
# Requires:
#   - SSH access to aur@aur.archlinux.org as a maintainer of the package
#     (works if your ~/.ssh/config has an IdentityFile that's listed on
#     your AUR profile — current setup uses ~/.ssh/aur_ed25519_bot).
#   - That a release matching $1 (default 0.1.0) is already published at
#     https://github.com/j4y-w4lk3r/fsvc/releases.

set -euo pipefail

VER="${1:-0.1.0}"

# Resolve to an absolute path BEFORE any cd, so the later `cd "$WORK"`
# doesn't break the relative lookup. Portable across macOS (no GNU
# realpath) and Linux.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKGBUILD_SRC="${SCRIPT_DIR}/PKGBUILD"

REPO="https://github.com/j4y-w4lk3r/fsvc"
AUR="ssh://aur@aur.archlinux.org/fsvc-bin.git"
WORK="/tmp/fsvc-bin-aur-bootstrap"

if [[ ! -f "$PKGBUILD_SRC" ]]; then
    echo "FATAL: can't find $PKGBUILD_SRC" >&2
    echo "       Run this script from the repo root or pass it the right path." >&2
    exit 1
fi

echo ">>> Cloning AUR repo (will be empty on first run)"
rm -rf "$WORK"
git clone "$AUR" "$WORK" 2>&1 | sed 's/^/    /'

cd "$WORK"

if [[ -n "$(git log --oneline 2>/dev/null || true)" ]]; then
    echo
    echo "[skip] AUR fsvc-bin repo already non-empty:"
    git log --oneline -3 | sed 's/^/    /'
    echo "       Use arch/aur-bump.sh (or just tag a release) for subsequent updates."
    exit 0
fi

echo ">>> Computing sha256 of release tarball for v${VER}"
url="${REPO}/releases/download/v${VER}/fsvc_${VER}_Linux_x86_64.tar.gz"
sha=$(curl -fsSL "$url" | sha256sum | awk '{print $1}')
echo "    sha256: $sha"

echo ">>> Rendering PKGBUILD"
sed \
    -e "s|^pkgver=.*|pkgver=${VER}|" \
    -e "s|^pkgrel=.*|pkgrel=1|" \
    -e "s|^sha256sums=.*|sha256sums=('${sha}')|" \
    "$PKGBUILD_SRC" > PKGBUILD

echo ">>> Hand-rendering .SRCINFO (no makepkg on macOS)"
cat > .SRCINFO <<EOF
pkgbase = fsvc-bin
	pkgdesc = Financial-services CLI: Revolut Business today, ING tomorrow
	pkgver = ${VER}
	pkgrel = 1
	url = ${REPO}
	arch = x86_64
	license = MIT
	depends = glibc
	optdepends = gnupg: fsvc bootstrap shells out to ykw which shells out to gpg
	optdepends = ykw: fsvc bootstrap shells out to ykw decrypt for the encrypted creds
	optdepends = bbm: fsvc bootstrap shells out to bbm pull to fetch encrypted creds from B2
	provides = fsvc
	conflicts = fsvc
	conflicts = fsvc-git
	source = fsvc-${VER}.tar.gz::${REPO}/releases/download/v${VER}/fsvc_${VER}_Linux_x86_64.tar.gz
	sha256sums = ${sha}

pkgname = fsvc-bin
EOF

echo ">>> Committing + pushing initial commit"
git -c user.name="j4y" -c user.email="j4y_w4lk3r@pobox.com" \
    add PKGBUILD .SRCINFO
git -c user.name="j4y" -c user.email="j4y_w4lk3r@pobox.com" \
    commit -m "Initial commit: fsvc-bin v${VER}

Financial-services CLI for the j4y suite. Binary package built from
the goreleaser linux-x86_64 release artifact. Subsequent versions are
auto-bumped by .github/workflows/release.yml in the upstream repo on
every git tag — see https://github.com/j4y-w4lk3r/fsvc."

# AUR uses 'master' as the canonical branch.
git branch -m master 2>/dev/null || true
git push -u origin master

echo
echo "[OK] fsvc-bin v${VER} bootstrapped on AUR."
echo "     https://aur.archlinux.org/packages/fsvc-bin"
echo "     Future tags auto-bump via the goreleaser aurs: block."
