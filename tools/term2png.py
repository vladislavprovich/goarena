#!/usr/bin/env python3
"""Render captured console output as a terminal-window "screenshot" PNG.

The input is a REAL log file captured from a command (for example
``go test -v ./... > results/logs/test_v.txt``). The tool only selects and
crops lines - it never edits their content - and draws them in a dark
terminal window with the command shown on a prompt line, so that the image
can be used as a figure in documentation.

Examples::

    python tools/term2png.py results/logs/test_v.txt fig.png \
        --cmd "go test -v ./..." --grep "^(ok|---|PASS|FAIL)" --max-lines 30

    python tools/term2png.py results/logs/bench.txt bench.png \
        --cmd "go test -bench . -benchmem ./..." --head 12 --tail 8

Line selection is applied in this order: --grep (keep matching lines),
--exclude (drop matching lines), then --head/--tail cropping (a marker line
"... (N lines omitted) ..." is inserted where lines were skipped) and
finally --max-lines. Lines longer than --cols are wrapped like a terminal
would wrap them. --squeeze collapses runs of three or more blanks (the
tabwriter padding of `go test -bench` output) into two spaces; it changes
only whitespace, never the text.
"""
import argparse
import os
import re
import sys

from PIL import Image, ImageDraw, ImageFont

FONT_CANDIDATES = [
    r"C:\Windows\Fonts\CascadiaMono.ttf",
    r"C:\Windows\Fonts\consola.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
    "DejaVuSansMono.ttf",
]
FONT_BOLD_CANDIDATES = [
    r"C:\Windows\Fonts\CascadiaMono.ttf",
    r"C:\Windows\Fonts\consolab.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
    "DejaVuSansMono-Bold.ttf",
]

# Colours (Windows Terminal "Campbell"-like dark scheme).
BG = (12, 12, 12)
TITLE_BG = (32, 32, 32)
TITLE_FG = (204, 204, 204)
FG = (204, 204, 204)
DIM = (118, 118, 118)
GREEN = (22, 198, 12)
RED = (231, 72, 86)
YELLOW = (249, 241, 165)
CYAN = (97, 214, 214)
BLUE = (59, 120, 255)
MAGENTA = (180, 0, 158)

RULES = [
    (re.compile(r"WARNING: DATA RACE|^FAIL|--- FAIL|panic:"), RED),
    (re.compile(r"^(ok|PASS)\b|--- PASS"), GREEN),
    (re.compile(r"coverage:|^total:"), CYAN),
    (re.compile(r"^=== RUN|^=== CONT|^=== PAUSE"), DIM),
    (re.compile(r"^fuzz:"), YELLOW),
    (re.compile(r"^Benchmark"), FG),
    (re.compile(r"^(goos|goarch|pkg|cpu):"), DIM),
    (re.compile(r"^\.\.\. \(\d+ lines omitted\) \.\.\.$"), MAGENTA),
]


def load_font(cands, size):
    for c in cands:
        try:
            return ImageFont.truetype(c, size)
        except OSError:
            continue
    return ImageFont.load_default()


def colour_for(line):
    for rx, col in RULES:
        if rx.search(line):
            return col
    return FG


def select_lines(lines, args):
    if args.squeeze:
        lines = [re.sub(r"[ 	]{3,}", "  ", l.expandtabs(8)) for l in lines]
    if args.grep:
        rx = re.compile(args.grep)
        lines = [l for l in lines if rx.search(l)]
    if args.exclude:
        rx = re.compile(args.exclude)
        lines = [l for l in lines if not rx.search(l)]
    if args.head is not None or args.tail is not None:
        h = args.head or 0
        t = args.tail or 0
        if h + t < len(lines):
            omitted = len(lines) - h - t
            lines = lines[:h] + ["... (%d lines omitted) ..." % omitted] + (lines[-t:] if t else [])
    if args.max_lines and len(lines) > args.max_lines:
        omitted = len(lines) - args.max_lines + 1
        lines = lines[: args.max_lines - 1] + ["... (%d lines omitted) ..." % omitted]
    return lines


def wrap(lines, cols):
    out = []
    for l in lines:
        l = l.expandtabs(4)
        if not l:
            out.append("")
        while l:
            out.append(l[:cols])
            l = l[cols:]
    return out


def render(lines, args):
    scale = args.scale
    fs = int(args.font_size * scale)
    font = load_font(FONT_CANDIDATES, fs)
    bold = load_font(FONT_BOLD_CANDIDATES, fs)
    cw = font.getbbox("M" * 10)[2] / 10.0
    lh = int(fs * 1.32)
    pad = int(14 * scale)
    title_h = int(30 * scale)

    prompt = args.prompt
    body = wrap(lines, args.cols)
    cmd_lines = wrap([prompt + args.cmd], args.cols)
    n_rows = len(cmd_lines) + len(body) + (1 if args.final_prompt else 0)
    width = int(pad * 2 + cw * args.cols)
    height = title_h + pad * 2 + lh * n_rows

    img = Image.new("RGB", (width, height), BG)
    d = ImageDraw.Draw(img)
    # title bar
    d.rectangle([0, 0, width, title_h], fill=TITLE_BG)
    tfont = load_font(FONT_CANDIDATES, int(12 * scale))
    d.text((pad, title_h / 2), args.title, font=tfont, fill=TITLE_FG, anchor="lm")
    # window buttons (minimise, maximise, close) drawn as simple glyphs
    bx = width - pad
    for glyph in ("x", "o", "-"):
        d.text((bx, title_h / 2), glyph, font=tfont, fill=TITLE_FG, anchor="rm")
        bx -= int(28 * scale)

    y = title_h + pad
    # prompt + command
    for i, cl in enumerate(cmd_lines):
        x = pad
        if i == 0:
            d.text((x, y), prompt, font=font, fill=GREEN)
            x += cw * len(prompt)
            d.text((x, y), cl[len(prompt):], font=bold, fill=YELLOW)
        else:
            d.text((x, y), cl, font=bold, fill=YELLOW)
        y += lh
    for l in body:
        d.text((pad, y), l, font=font, fill=colour_for(l))
        y += lh
    if args.final_prompt:
        d.text((pad, y), prompt.rstrip() + " ", font=font, fill=GREEN)
        cx = pad + cw * len(prompt.rstrip() + " ")
        d.rectangle([cx, y + 2, cx + cw * 0.9, y + lh - 3], fill=FG)
    return img


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("input", help="captured console output (UTF-8 text)")
    p.add_argument("output", help="PNG file to write")
    p.add_argument("--cmd", required=True, help="command shown on the prompt line")
    p.add_argument("--prompt", default="PS D:\\master-diploma\\goarena> ", help="prompt text")
    p.add_argument("--title", default="Windows PowerShell", help="window title")
    p.add_argument("--grep", help="keep only lines matching this regex")
    p.add_argument("--exclude", help="drop lines matching this regex")
    p.add_argument("--head", type=int, help="keep the first N selected lines")
    p.add_argument("--tail", type=int, help="keep the last N selected lines")
    p.add_argument("--max-lines", type=int, default=35, help="hard limit of body lines")
    p.add_argument("--squeeze", action="store_true", help="collapse runs of >= 3 blanks into two spaces")
    p.add_argument("--cols", type=int, default=95, help="terminal width in columns")
    p.add_argument("--font-size", type=float, default=14, help="font size in px at scale 1")
    p.add_argument("--scale", type=float, default=2.0, help="supersampling factor")
    p.add_argument("--no-final-prompt", dest="final_prompt", action="store_false")
    p.add_argument("--dpi", type=int, default=200)
    args = p.parse_args(argv)

    with open(args.input, encoding="utf-8", errors="replace") as f:
        raw = f.read().replace("\r\n", "\n").replace("\r", "\n")
    lines = raw.split("\n")
    while lines and not lines[-1].strip():
        lines.pop()
    lines = select_lines(lines, args)
    img = render(lines, args)
    os.makedirs(os.path.dirname(os.path.abspath(args.output)), exist_ok=True)
    img.save(args.output, dpi=(args.dpi, args.dpi))
    print("wrote %s (%dx%d px, %d lines)" % (args.output, img.width, img.height, len(lines)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
