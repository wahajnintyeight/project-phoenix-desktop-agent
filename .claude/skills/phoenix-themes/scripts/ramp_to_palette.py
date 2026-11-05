#!/usr/bin/env python3
"""Turn colour ramps pasted from Coolors or Tailwind into a Phoenix palette entry.

    python ramp_to_palette.py ramps.json --id forest --name "Forest" --note "Moss and bark" \
        --neutral bark --accent moss [--light-neutral linen] [--light-accent moss]

ramps.json holds the ramps as exported, e.g. {"moss": {"DEFAULT": "#84a59d", "100": "#192220", ...
"900": "#e6edeb"}}. Single quotes and trailing commas are tolerated (pasted JS objects).

In an export, 100 is the darkest step and 900 the lightest, which is the order Phoenix wants
(darkest to lightest), so the nine steps are used as they come. The entry is printed for you to
paste into PALETTES in ui/src/themes.ts. It is a starting point: the same ramp serves both modes,
which works when the neutral ramp runs from a near-black to a near-white. Then run
check-contrast.mjs and tune (see references/best-practices.md).
"""
import argparse
import json
import re
import sys

STEPS = ["100", "200", "300", "400", "500", "600", "700", "800", "900"]
HEX = re.compile(r"^#[0-9a-fA-F]{6}$")


def load(path):
    text = open(path, encoding="utf-8").read()
    text = text.replace("'", '"')
    text = re.sub(r",\s*([}\]])", r"\1", text)  # trailing commas
    text = re.sub(r"([{,]\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*:)", r'\1"\2"\3', text)  # bare keys
    text = re.sub(r"([{,]\s*)(\d+)(\s*:)", r'\1"\2"\3', text)  # numeric keys
    return json.loads(text)


def ramp(data, key):
    if key not in data:
        sys.exit(f"no ramp named {key!r}; have: {', '.join(data)}")
    r = data[key]
    out = []
    for s in STEPS:
        v = str(r.get(s, "")).lower()
        if not HEX.match(v):
            sys.exit(f"{key}[{s}] is {v!r}, expected a #rrggbb colour")
        out.append(v)
    return out


def fmt(r):
    return "[" + ", ".join(f'"{c}"' for c in r) + "]"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("file")
    ap.add_argument("--id", required=True, help="lowercase id, stored in localStorage (e.g. forest)")
    ap.add_argument("--name", required=True, help="name shown in the picker")
    ap.add_argument("--note", required=True, help="a few words under the name")
    ap.add_argument("--neutral", required=True, help="ramp for surfaces and text (dark mode)")
    ap.add_argument("--accent", required=True, help="ramp for the accent (dark mode)")
    ap.add_argument("--light-neutral", help="neutral ramp for light mode (default: --neutral)")
    ap.add_argument("--light-accent", help="accent ramp for light mode (default: --accent)")
    a = ap.parse_args()

    if not re.fullmatch(r"[a-z][a-z0-9-]{1,23}", a.id):
        sys.exit("--id must be 2-24 lowercase letters, digits or hyphens, starting with a letter")
    data = load(a.file)
    nd, ad = ramp(data, a.neutral), ramp(data, a.accent)
    nl, al = ramp(data, a.light_neutral or a.neutral), ramp(data, a.light_accent or a.accent)

    print("  {")
    print(f'    id: "{a.id}",')
    print(f'    name: "{a.name}",')
    print(f'    note: "{a.note}",')
    print(f"    n: {{\n      dark: {fmt(nd)},\n      light: {fmt(nl)},\n    }},")
    print(f"    a: {{\n      dark: {fmt(ad)},\n      light: {fmt(al)},\n    }},")
    print("  },")
    print(
        "\nPaste into PALETTES in ui/src/themes.ts, then run scripts/check-contrast.mjs " + a.id,
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
