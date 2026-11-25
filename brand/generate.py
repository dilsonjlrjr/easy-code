"""Generates every Easy Code icon from the SVG masters in this folder (run with `make -C project icons`).

Needs rsvg-convert and the Inter font (the Makefile runs it in a pinned Alpine container). ICO and ICNS are
written here with PNG entries, so no other image tool is needed. Outputs are committed: builds never render.
"""

import os
import struct
import subprocess
import sys
import tempfile

BRAND = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(BRAND)
WEB = os.path.join(ROOT, "frontend", "public")
DESKTOP = os.path.join(ROOT, "desktop", "build")

ICON = os.path.join(BRAND, "icon.svg")
SMALL = os.path.join(BRAND, "icon-small.svg")
TRAY = os.path.join(BRAND, "tray.svg")
TEMPLATE = os.path.join(BRAND, "tray-template.svg")


def render(svg: str, size: int) -> bytes:
    """PNG of the SVG at size × size."""
    return subprocess.run(
        ["rsvg-convert", "-w", str(size), "-h", str(size), "-f", "png", svg],
        check=True,
        capture_output=True,
    ).stdout


def full_bleed(size: int) -> bytes:
    """The app icon without the transparent margin of the macOS grid (iOS and Android crop it themselves)."""
    with open(ICON, encoding="utf-8") as source:
        text = source.read().replace('viewBox="0 0 1024 1024"', 'viewBox="100 100 824 824"', 1)
    with tempfile.NamedTemporaryFile("w", suffix=".svg", delete=False, encoding="utf-8") as cropped:
        cropped.write(text)
    try:
        return render(cropped.name, size)
    finally:
        os.unlink(cropped.name)


def icon_for(size: int) -> bytes:
    """The small master up to 32 px, where the binary digits would blur; the full one above."""
    return render(SMALL if size <= 32 else ICON, size)


def ico(entries: list[tuple[int, bytes]]) -> bytes:
    """Windows ICO with PNG entries (supported since Windows Vista and by every browser)."""
    header = struct.pack("<HHH", 0, 1, len(entries))
    offset = len(header) + 16 * len(entries)
    directory, images = b"", b""
    for size, png in entries:
        side = 0 if size >= 256 else size
        directory += struct.pack("<BBBBHHII", side, side, 0, 0, 1, 32, len(png), offset + len(images))
        images += png
    return header + directory + images


# ICNS element types with PNG payloads and their pixel sizes (Apple Icon Image format).
ICNS_TYPES = [
    (b"icp4", 16),
    (b"icp5", 32),
    (b"ic11", 32),
    (b"icp6", 64),
    (b"ic12", 64),
    (b"ic07", 128),
    (b"ic08", 256),
    (b"ic13", 256),
    (b"ic09", 512),
    (b"ic14", 512),
    (b"ic10", 1024),
]


def icns() -> bytes:
    body = b""
    for kind, size in ICNS_TYPES:
        png = icon_for(size)
        body += kind + struct.pack(">I", 8 + len(png)) + png
    return b"icns" + struct.pack(">I", 8 + len(body)) + body


def write(path: str, data: bytes) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as out:
        out.write(data)
    print(os.path.relpath(path, ROOT))


def main() -> int:
    # Web (frontend/index.html): favicon, Apple touch icon and the manifest icons.
    write(os.path.join(WEB, "favicon.ico"), ico([(size, icon_for(size)) for size in (16, 32, 48)]))
    with open(SMALL, "rb") as small:
        write(os.path.join(WEB, "favicon.svg"), small.read())
    write(os.path.join(WEB, "apple-touch-icon.png"), full_bleed(180))
    write(os.path.join(WEB, "icon-192.png"), full_bleed(192))
    write(os.path.join(WEB, "icon-512.png"), full_bleed(512))

    # Desktop (Wails): window and notification icon, tray, macOS bundle, Windows executable, Linux desktop entry.
    with open(SMALL, "rb") as small:
        write(os.path.join(ROOT, "desktop", "ui", "public", "logo.svg"), small.read())
    write(os.path.join(DESKTOP, "appicon.png"), render(ICON, 1024))
    write(os.path.join(DESKTOP, "tray.png"), render(TRAY, 64))
    write(os.path.join(DESKTOP, "tray-template.png"), render(TEMPLATE, 44))
    write(os.path.join(DESKTOP, "darwin", "icons.icns"), icns())
    write(
        os.path.join(DESKTOP, "windows", "icon.ico"),
        ico([(size, icon_for(size)) for size in (16, 24, 32, 48, 64, 128, 256)]),
    )
    for size in (16, 32, 48, 64, 128, 256, 512):
        write(
            os.path.join(DESKTOP, "linux", "icons", "hicolor", f"{size}x{size}", "apps", "easycode-desktop.png"),
            icon_for(size),
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
