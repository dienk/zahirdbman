#!/usr/bin/env bash
# Build backend installer packages for macOS, Ubuntu/Debian Linux and Windows.
#
#   macOS   -> dist/zahirdbman-<ver>-macos.pkg        (universal, pkgbuild)
#   Ubuntu  -> dist/zahirdbman_<ver>_amd64.deb        (built with ar + tar)
#   Windows -> dist/zahirdbman-<ver>-windows-amd64.zip (exe + install.ps1)
#
# Usage: scripts/build-installers.sh [version]   (default version below)
set -euo pipefail

VERSION="${1:-0.1.0}"
PKG_ID="co.zahir.zahirdbman"
MAINTAINER="Zahir <itofficer@zahir.co.id>"
HOMEPAGE="https://github.com/dienk/zahirdbman"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Use the Command Line Tools for Apple utilities (lipo/pkgbuild) when the
# selected Xcode is missing its SDK, which otherwise breaks these tools.
if [ -z "${DEVELOPER_DIR:-}" ] && [ -d /Library/Developer/CommandLineTools ]; then
  export DEVELOPER_DIR=/Library/Developer/CommandLineTools
fi
DIST="$ROOT/dist"
BUILD="$ROOT/build"
rm -rf "$DIST" "$BUILD"
mkdir -p "$DIST" "$BUILD"

LDFLAGS="-s -w -X main.version=${VERSION}"
gobuild() { # $1=GOOS $2=GOARCH $3=output
  echo "  building $1/$2"
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$3" ./cmd/server
}

echo "==> Cross-compiling binaries (v$VERSION)"
gobuild darwin  amd64 "$BUILD/zahirdbman-darwin-amd64"
gobuild darwin  arm64 "$BUILD/zahirdbman-darwin-arm64"
gobuild linux   amd64 "$BUILD/zahirdbman-linux-amd64"
gobuild windows amd64 "$BUILD/zahirdbman-windows-amd64.exe"

# ---------------------------------------------------------------- macOS .pkg
echo "==> macOS: universal binary + .pkg"
lipo -create -output "$BUILD/zahirdbman-darwin-universal" \
  "$BUILD/zahirdbman-darwin-amd64" "$BUILD/zahirdbman-darwin-arm64"
MAC_ROOT="$BUILD/macpkg-root"
mkdir -p "$MAC_ROOT/usr/local/bin"
cp "$BUILD/zahirdbman-darwin-universal" "$MAC_ROOT/usr/local/bin/zahirdbman"
chmod 0755 "$MAC_ROOT/usr/local/bin/zahirdbman"
pkgbuild --root "$MAC_ROOT" \
  --identifier "$PKG_ID" --version "$VERSION" \
  --install-location / \
  "$DIST/zahirdbman-${VERSION}-macos.pkg" >/dev/null
echo "    -> dist/zahirdbman-${VERSION}-macos.pkg"

# ---------------------------------------------------------------- Ubuntu .deb
echo "==> Ubuntu/Debian: .deb (amd64)"
DEB="$BUILD/deb"
rm -rf "$DEB"; mkdir -p "$DEB/data/usr/bin" "$DEB/data/lib/systemd/system" \
  "$DEB/data/usr/share/doc/zahirdbman" "$DEB/data/etc/zahirdbman" "$DEB/control"
cp "$BUILD/zahirdbman-linux-amd64" "$DEB/data/usr/bin/zahirdbman"
chmod 0755 "$DEB/data/usr/bin/zahirdbman"

cat > "$DEB/data/lib/systemd/system/zahirdbman.service" <<UNIT
[Unit]
Description=zahirdbman PostgreSQL web manager
After=network.target

[Service]
DynamicUser=yes
StateDirectory=zahirdbman
Environment=ZDBM_ADDR=:8080
Environment=ZDBM_PROFILES=/var/lib/zahirdbman/profiles.json
EnvironmentFile=-/etc/zahirdbman/zahirdbman.env
ExecStart=/usr/bin/zahirdbman
Restart=on-failure

[Install]
WantedBy=multi-user.target
UNIT

cat > "$DEB/data/etc/zahirdbman/zahirdbman.env.example" <<ENV
# Copy to zahirdbman.env and edit, then: systemctl enable --now zahirdbman
PGHOST=localhost
PGPORT=5432
PGUSER=postgres
PGPASSWORD=
PGSSLMODE=prefer
ZDBM_ADMIN_DB=postgres
ENV
cp "$ROOT/README.md" "$DEB/data/usr/share/doc/zahirdbman/README.md"

INSTALLED_SIZE=$(( $(du -sk "$DEB/data" | cut -f1) ))
cat > "$DEB/control/control" <<CTRL
Package: zahirdbman
Version: ${VERSION}
Section: database
Priority: optional
Architecture: amd64
Maintainer: ${MAINTAINER}
Depends: postgresql-client
Installed-Size: ${INSTALLED_SIZE}
Homepage: ${HOMEPAGE}
Description: Web-based PostgreSQL database manager
 zahirdbman is a self-contained web UI for managing PostgreSQL: browse
 databases and tables, run SQL, back up and restore, and manage saved
 connection profiles. Ships a systemd service (disabled by default).
CTRL

cat > "$DEB/control/postinst" <<'POST'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  echo "zahirdbman installed. Configure /etc/zahirdbman/zahirdbman.env then:"
  echo "  sudo systemctl enable --now zahirdbman"
fi
exit 0
POST
chmod 0755 "$DEB/control/postinst"

# Assemble the .deb. A .deb is an ar archive of debian-binary, control.tar.gz
# and data.tar.gz. macOS's ar writes a BSD archive with a symbol table that
# dpkg rejects, so we write the exact Debian ar format ourselves.
echo "2.0" > "$DEB/debian-binary"
TAR_OWN="--uid 0 --gid 0 --uname root --gname root"
( cd "$DEB/control" && tar $TAR_OWN -czf "$DEB/control.tar.gz" ./control ./postinst )
( cd "$DEB/data"    && tar $TAR_OWN -czf "$DEB/data.tar.gz" ./usr ./lib ./etc )
DEB_OUT="$DIST/zahirdbman_${VERSION}_amd64.deb"
python3 - "$DEB_OUT" "$DEB/debian-binary" "$DEB/control.tar.gz" "$DEB/data.tar.gz" <<'PY'
import sys, os
out, files = sys.argv[1], sys.argv[2:]
with open(out, "wb") as f:
    f.write(b"!<arch>\n")
    for path in files:
        data = open(path, "rb").read()
        name = os.path.basename(path)
        hdr = "%-16s%-12d%-6d%-6d%-8s%-10d`\n" % (name, 0, 0, 0, "100644", len(data))
        assert len(hdr) == 60, len(hdr)
        f.write(hdr.encode())
        f.write(data)
        if len(data) % 2 == 1:
            f.write(b"\n")
print("wrote", out)
PY
echo "    -> $(basename "$DEB_OUT")"

# ---------------------------------------------------------------- Windows .zip
echo "==> Windows: zip installer (exe + install.ps1)"
WIN="$BUILD/win"
rm -rf "$WIN"; mkdir -p "$WIN"
cp "$BUILD/zahirdbman-windows-amd64.exe" "$WIN/zahirdbman.exe"

cat > "$WIN/install.ps1" <<'PS1'
# zahirdbman installer for Windows. Right-click > Run with PowerShell,
# or:  powershell -ExecutionPolicy Bypass -File install.ps1
$ErrorActionPreference = "Stop"
$dest = Join-Path $env:LOCALAPPDATA "Programs\zahirdbman"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item -Path (Join-Path $PSScriptRoot "zahirdbman.exe") -Destination $dest -Force
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dest*") {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$dest", "User")
  Write-Host "Added to PATH (open a new terminal to pick it up)."
}
Write-Host "Installed zahirdbman to $dest"
Write-Host "Run it with:  zahirdbman   (then open http://localhost:8080)"
PS1

cat > "$WIN/README.txt" <<TXT
zahirdbman ${VERSION} - Windows

Install:
  1. Unzip this folder.
  2. Right-click install.ps1 -> Run with PowerShell
     (or: powershell -ExecutionPolicy Bypass -File install.ps1)
  3. Open a new terminal and run:  zahirdbman
  4. Browse to http://localhost:8080

Configure the PostgreSQL connection with environment variables
(PGHOST, PGPORT, PGUSER, PGPASSWORD, ...) - see the project README.

Backup & Restore requires the psql client tools (pg_dump/pg_restore/psql)
on PATH; install "PostgreSQL" or its client package for that feature.
TXT

WIN_OUT="$DIST/zahirdbman-${VERSION}-windows-amd64.zip"
( cd "$WIN" && zip -q -r "$WIN_OUT" . )
echo "    -> $(basename "$WIN_OUT")"

# ---------------------------------------------------------------- checksums
echo "==> Checksums"
( cd "$DIST" && shasum -a 256 * > SHA256SUMS.txt )

echo
echo "Done. Artifacts in dist/:"
ls -lh "$DIST" | awk 'NR>1 {print "  "$9"  "$5}'
