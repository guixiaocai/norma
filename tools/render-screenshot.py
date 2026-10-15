#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Render the actual deterministic dashboard ANSI frame to a documentation PNG.

Requires Pillow. No mock UI is drawn: every cell comes from model.View().
  NORMA_SCREENSHOT=/tmp/norma.ansi go test ./internal/app -run TestDashboardScreenshot -count=1
  python3 tools/render-screenshot.py /tmp/norma.ansi docs/images/dashboard.png
"""

import argparse
import math
import re
import unicodedata
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


def indexed_color(index):
    base = ["#000000", "#cd0000", "#00cd00", "#cdcd00", "#0000ee", "#cd00cd",
            "#00cdcd", "#e5e5e5", "#7f7f7f", "#ff0000", "#00ff00", "#ffff00",
            "#5c5cff", "#ff00ff", "#00ffff", "#ffffff"]
    if index < 16:
        return base[index]
    if index >= 232:
        value = 8 + (index - 232) * 10
        return (value, value, value)
    index -= 16
    levels = [0, 95, 135, 175, 215, 255]
    return (levels[index // 36], levels[(index // 6) % 6], levels[index % 6])


def cells(line):
    color, bold = "#e2e8f0", False
    for token in re.split(r"(\x1b\[[0-9;]*m)", line):
        if token.startswith("\x1b["):
            codes = [int(v or 0) for v in token[2:-1].split(";")]
            i = 0
            while i < len(codes):
                code = codes[i]
                if code == 0:
                    color, bold = "#e2e8f0", False
                elif code == 1:
                    bold = True
                elif code == 22:
                    bold = False
                elif code == 39:
                    color = "#e2e8f0"
                elif code == 38 and codes[i + 1] == 5:
                    color = indexed_color(codes[i + 2])
                    i += 2
                elif 30 <= code <= 37:
                    color = indexed_color(code - 30)
                else:
                    raise ValueError(f"Unsupported SGR code: {codes}")
                i += 1
        else:
            for char in token:
                if ord(char) < 32:
                    raise ValueError("Expected a static frame containing only text and SGR colors")
                width = 0 if unicodedata.combining(char) else (2 if unicodedata.east_asian_width(char) in "WF" else 1)
                yield char, color, bold, width


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("frame", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--font", default="/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf")
    parser.add_argument("--bold-font", default="/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf")
    args = parser.parse_args()
    lines = [list(cells(line)) for line in args.frame.read_text().splitlines()]
    font = ImageFont.truetype(args.font, 18)
    bold = ImageFont.truetype(args.bold_font, 18)
    cell_width, cell_height, margin = math.ceil(font.getlength("M")), font.getbbox("│")[3], 28
    columns = max(sum(cell[3] for cell in line) for line in lines)
    image = Image.new("RGB", (columns * cell_width + margin * 2, len(lines) * cell_height + margin * 2), "#101722")
    draw = ImageDraw.Draw(image)
    for row, line in enumerate(lines):
        column = 0
        for char, color, strong, width in line:
            draw.text((margin + column * cell_width, margin + row * cell_height), char,
                      fill=color, font=bold if strong else font)
            column += width
    args.output.parent.mkdir(parents=True, exist_ok=True)
    image.save(args.output)
    print(f"Rendered {columns} x {len(lines)} actual terminal cells to {args.output}")


if __name__ == "__main__":
    main()
