#!/usr/bin/env python3
"""Generate a flat, shields.io-style SVG badge as a plain text file.

No network calls and no third-party badge service/gist dependency — the
coverage workflow (.github/workflows/coverage.yml) runs this and commits
the result straight into the repo, so the README's coverage badge is a
real, workflow-updated file rather than a hardcoded claim or a dependency
on an external badge host staying up.

Usage: make_badge.py <label> <message> <color> <output_path>
color is any full CSS color value SVG's fill accepts, e.g. '#4c1' or
'red' — include the '#' for hex, it isn't added automatically.
"""
import sys

# Same rough per-character width shields.io's own flat badges use for
# Verdana 11px — not pixel-perfect, but close enough that labels/messages
# of any length don't visibly overlap the badge's rounded background.
CHAR_WIDTH = 6.5
PAD = 10


def text_width(s: str) -> int:
    return round(len(s) * CHAR_WIDTH) + PAD


def make_badge(label: str, message: str, color: str) -> str:
    label_w = text_width(label)
    message_w = text_width(message)
    total_w = label_w + message_w

    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{total_w}" height="20" role="img" aria-label="{label}: {message}">
  <linearGradient id="s" x2="0" y2="100%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r">
    <rect width="{total_w}" height="20" rx="3" fill="#fff"/>
  </clipPath>
  <g clip-path="url(#r)">
    <rect width="{label_w}" height="20" fill="#555"/>
    <rect x="{label_w}" width="{message_w}" height="20" fill="{color}"/>
    <rect width="{total_w}" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,sans-serif" font-size="11">
    <text x="{label_w / 2}" y="14">{label}</text>
    <text x="{label_w + message_w / 2}" y="14">{message}</text>
  </g>
</svg>
'''


def main() -> int:
    if len(sys.argv) != 5:
        print(f"usage: {sys.argv[0]} <label> <message> <color> <output_path>", file=sys.stderr)
        return 1

    label, message, color, output_path = sys.argv[1:5]
    with open(output_path, "w") as f:
        f.write(make_badge(label, message, color))
    print(f"wrote {output_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
