#!/bin/sh
# Trangly installer — https://trangly.dev/install.sh
# Usage: curl -fsSL https://trangly.dev/install.sh | sh
#
# What this script does:
#   1. Detects your OS (Debian/Ubuntu, RHEL/Fedora/CentOS, Alpine) and architecture
#   2. Downloads the latest Trangly package from GitHub Releases
#   3. Installs it via the native package manager
#   4. Runs `trangly check` to show the preflight status
#
# Requires: curl or wget, root/sudo access.
# Supports: Linux only (amd64, arm64).

set -e

REPO="udaypankhaniya/trangly"
BINARY_NAME="trangly"
INSTALL_DIR="/usr/local/bin"

# ── Helpers ──────────────────────────────────────────────────────────────────

info()  { printf '\033[1;34m[info]\033[0m  %s\n' "$1"; }
ok()    { printf '\033[1;32m[ok]\033[0m    %s\n' "$1"; }
warn()  { printf '\033[1;33m[warn]\033[0m  %s\n' "$1"; }
err()   { printf '\033[1;31m[error]\033[0m %s\n' "$1" >&2; }
die()   { err "$1"; exit 1; }

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

# ── Root check ───────────────────────────────────────────────────────────────

ensure_root() {
    if [ "$(id -u)" -ne 0 ]; then
        if command -v sudo >/dev/null 2>&1; then
            SUDO="sudo"
        else
            die "This script must be run as root (or install sudo)."
        fi
    else
        SUDO=""
    fi
}

# ── Detect OS and package manager ────────────────────────────────────────────

detect_os() {
    if [ ! -f /etc/os-release ]; then
        die "Cannot detect OS — /etc/os-release not found. Trangly supports Debian/Ubuntu, RHEL/Fedora/CentOS, and Alpine Linux."
    fi

    . /etc/os-release

    case "$ID" in
        debian|ubuntu|linuxmint|pop|raspbian)
            PKG_TYPE="deb"
            ;;
        rhel|centos|fedora|rocky|almalinux|ol|amzn)
            PKG_TYPE="rpm"
            ;;
        alpine)
            PKG_TYPE="apk"
            ;;
        *)
            # Try ID_LIKE as fallback
            case "$ID_LIKE" in
                *debian*|*ubuntu*)  PKG_TYPE="deb" ;;
                *rhel*|*fedora*)    PKG_TYPE="rpm" ;;
                *)                  die "Unsupported OS: $PRETTY_NAME ($ID). Trangly supports Debian/Ubuntu, RHEL/Fedora/CentOS, and Alpine." ;;
            esac
            ;;
    esac

    info "Detected OS: $PRETTY_NAME → package type: .$PKG_TYPE"
}

# ── Detect architecture ─────────────────────────────────────────────────────

detect_arch() {
    UNAME_ARCH=$(uname -m)
    case "$UNAME_ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        *)              die "Unsupported architecture: $UNAME_ARCH. Trangly supports amd64 and arm64." ;;
    esac
    info "Detected architecture: $ARCH"
}

# ── Fetch latest release tag from GitHub ─────────────────────────────────────

fetch_latest_version() {
    RELEASES_URL="https://api.github.com/repos/${REPO}/releases/latest"

    if command -v curl >/dev/null 2>&1; then
        RESPONSE=$(curl -fsSL "$RELEASES_URL" 2>/dev/null) || die "Failed to query GitHub API. Check your network connection."
    elif command -v wget >/dev/null 2>&1; then
        RESPONSE=$(wget -qO- "$RELEASES_URL" 2>/dev/null) || die "Failed to query GitHub API. Check your network connection."
    else
        die "Neither curl nor wget found. Install one and retry."
    fi

    # Extract tag_name from JSON without jq (portable).
    VERSION=$(printf '%s' "$RESPONSE" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$VERSION" ]; then
        die "Could not determine the latest release version. Check https://github.com/${REPO}/releases"
    fi

    # Strip leading 'v' if present for the package filename.
    VERSION_NUM=$(printf '%s' "$VERSION" | sed 's/^v//')

    info "Latest version: $VERSION"
}

# ── Build download URL ───────────────────────────────────────────────────────

build_download_url() {
    # nfpm produces filenames like: trangly_1.0.0_amd64.deb, trangly-1.0.0-1.x86_64.rpm, trangly_1.0.0_aarch64.apk
    case "$PKG_TYPE" in
        deb)
            FILENAME="${BINARY_NAME}_${VERSION_NUM}_${ARCH}.deb"
            ;;
        rpm)
            RPM_ARCH="$ARCH"
            [ "$ARCH" = "amd64" ] && RPM_ARCH="x86_64"
            [ "$ARCH" = "arm64" ] && RPM_ARCH="aarch64"
            FILENAME="${BINARY_NAME}-${VERSION_NUM}-1.${RPM_ARCH}.rpm"
            ;;
        apk)
            APK_ARCH="$ARCH"
            [ "$ARCH" = "amd64" ] && APK_ARCH="x86_64"
            [ "$ARCH" = "arm64" ] && APK_ARCH="aarch64"
            FILENAME="${BINARY_NAME}_${VERSION_NUM}_${APK_ARCH}.apk"
            ;;
    esac

    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
    info "Package URL: $DOWNLOAD_URL"
}

# ── Download ─────────────────────────────────────────────────────────────────

download_package() {
    TMP_DIR=$(mktemp -d)
    TMP_FILE="${TMP_DIR}/${FILENAME}"

    info "Downloading ${FILENAME}..."
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL" || die "Download failed. Verify the release exists: $DOWNLOAD_URL"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$TMP_FILE" "$DOWNLOAD_URL" || die "Download failed. Verify the release exists: $DOWNLOAD_URL"
    fi
    ok "Downloaded to $TMP_FILE"
}

# ── Install ──────────────────────────────────────────────────────────────────

install_package() {
    info "Installing ${FILENAME}..."
    case "$PKG_TYPE" in
        deb)
            $SUDO dpkg -i "$TMP_FILE" || { $SUDO apt-get install -f -y && $SUDO dpkg -i "$TMP_FILE"; }
            ;;
        rpm)
            $SUDO rpm -U --force "$TMP_FILE"
            ;;
        apk)
            $SUDO apk add --allow-untrusted "$TMP_FILE"
            ;;
    esac
    ok "Trangly $VERSION installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

# ── Cleanup ──────────────────────────────────────────────────────────────────

cleanup() {
    rm -rf "$TMP_DIR" 2>/dev/null || true
}

# ── Post-install ─────────────────────────────────────────────────────────────

post_install() {
    printf '\n'
    info "Running preflight checks..."
    ${INSTALL_DIR}/${BINARY_NAME} check || true

    printf '\n'
    ok "Installation complete!"
    printf '\n'
    printf '  Next steps:\n'
    printf '    1. Run:  trangly setup\n'
    printf '    2. Open: http://your-server-ip:2880\n'
    printf '    3. Click "Connect GitHub" and add your first project\n'
    printf '\n'
    printf '  Docs: https://github.com/%s\n' "$REPO"
    printf '\n'
}

# ── Main ─────────────────────────────────────────────────────────────────────

main() {
    printf '\n'
    printf '  \033[1mTrangly Installer\033[0m\n'
    printf '  Zero YAML. Zero cloud accounts. Zero DevOps.\n'
    printf '\n'

    ensure_root
    detect_os
    detect_arch
    fetch_latest_version
    build_download_url
    download_package
    install_package
    cleanup
    post_install
}

main
