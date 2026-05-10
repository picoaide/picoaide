#!/usr/bin/env python3
"""Sync PicoAide brand assets from the single source directory."""

from __future__ import annotations

import struct
import zlib
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BRAND = ROOT / "assets" / "brand"


def write_text(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def copy_text(source: Path, target: Path) -> None:
    write_text(target, source.read_text(encoding="utf-8"))


def png_chunk(kind: bytes, data: bytes) -> bytes:
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)


def rgba_png(path: Path, size: int) -> None:
    """Create a crisp app icon PNG without external image dependencies."""
    path.parent.mkdir(parents=True, exist_ok=True)
    pixels = bytearray()
    radius = round(size * 0.23)
    pad = max(1, round(size * 0.04))
    bg1 = (15, 23, 42)
    bg2 = (22, 59, 115)
    cyan = (94, 234, 212)
    blue = (96, 165, 250)
    white = (248, 250, 252)

    def inside_round_rect(x: int, y: int) -> bool:
        left, top, right, bottom = pad, pad, size - pad - 1, size - pad - 1
        if left + radius <= x <= right - radius and top <= y <= bottom:
            return True
        if left <= x <= right and top + radius <= y <= bottom - radius:
            return True
        corners = (
            (left + radius, top + radius),
            (right - radius, top + radius),
            (left + radius, bottom - radius),
            (right - radius, bottom - radius),
        )
        return any((x - cx) * (x - cx) + (y - cy) * (y - cy) <= radius * radius for cx, cy in corners)

    def in_rect(x: int, y: int, x1: float, y1: float, x2: float, y2: float) -> bool:
        return round(x1 * size) <= x <= round(x2 * size) and round(y1 * size) <= y <= round(y2 * size)

    def in_circle(x: int, y: int, cx: float, cy: float, r: float) -> bool:
        dx = x - cx * size
        dy = y - cy * size
        return dx * dx + dy * dy <= (r * size) * (r * size)

    for y in range(size):
        pixels.append(0)
        for x in range(size):
            if not inside_round_rect(x, y):
                pixels.extend((0, 0, 0, 0))
                continue

            t = (x + y) / max(1, 2 * size - 2)
            r = round(bg1[0] * (1 - t) + bg2[0] * t)
            g = round(bg1[1] * (1 - t) + bg2[1] * t)
            b = round(bg1[2] * (1 - t) + bg2[2] * t)
            color = (r, g, b, 255)

            # Monogram approximates the SVG P shape at small favicon sizes.
            if (
                in_rect(x, y, 0.32, 0.28, 0.42, 0.70)
                or in_rect(x, y, 0.32, 0.28, 0.62, 0.38)
                or in_rect(x, y, 0.56, 0.34, 0.67, 0.50)
                or in_rect(x, y, 0.32, 0.47, 0.62, 0.57)
            ):
                color = (*white, 255)

            if in_rect(x, y, 0.27, 0.74, 0.73, 0.80):
                color = (*blue, 255)
            if in_rect(x, y, 0.27, 0.22, 0.60, 0.28):
                color = (*cyan, 255)
            if in_circle(x, y, 0.75, 0.27, 0.075):
                color = (*cyan, 255)
            if in_circle(x, y, 0.75, 0.73, 0.075):
                color = (*blue, 255)

            pixels.extend(color)

    raw = bytes(pixels)
    png = (
        b"\x89PNG\r\n\x1a\n"
        + png_chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0))
        + png_chunk(b"IDAT", zlib.compress(raw, 9))
        + png_chunk(b"IEND", b"")
    )
    path.write_bytes(png)


def main() -> None:
    mark = BRAND / "logo-mark.svg"
    horizontal = BRAND / "logo-horizontal.svg"

    copy_text(horizontal, ROOT / "website" / "static" / "images" / "logo.svg")
    copy_text(mark, ROOT / "website" / "static" / "images" / "logo-mark.svg")
    copy_text(mark, ROOT / "website" / "static" / "favicon.svg")
    copy_text(mark, ROOT / "internal" / "web" / "ui" / "images" / "logo-mark.svg")

    for size in (16, 48, 128):
        rgba_png(ROOT / "picoaide-extension" / "icons" / f"icon{size}.png", size)


if __name__ == "__main__":
    main()
