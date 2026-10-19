"""Stitch the frames from readme_demo_test.go into the README animation.

    python docs/make_demo_gif.py <frames dir> docs/demo.gif
"""
import sys
from pathlib import Path

from PIL import Image

WIDTH = 960      # README column is ~880px; a little over keeps text crisp on hi-dpi
HOLD_MS = 2200   # time on each tab
FADE_STEPS = 6
FADE_MS = 50


def main(src, dst):
    frames = sorted(Path(src).glob("*.png"))
    shots = []
    for f in frames:
        im = Image.open(f).convert("RGB")
        h = round(im.height * WIDTH / im.width)
        shots.append(im.resize((WIDTH, h), Image.LANCZOS))

    seq, durations = [], []
    for i, cur in enumerate(shots):
        seq.append(cur)
        durations.append(HOLD_MS)
        nxt = shots[(i + 1) % len(shots)]
        for k in range(1, FADE_STEPS):
            seq.append(Image.blend(cur, nxt, k / FADE_STEPS))
            durations.append(FADE_MS)

    # One shared palette keeps the dark background from dithering frame to frame.
    pal = seq[0].quantize(colors=255, method=Image.Quantize.MEDIANCUT)
    out = [im.quantize(palette=pal, dither=Image.Dither.NONE) for im in seq]
    out[0].save(dst, save_all=True, append_images=out[1:], duration=durations,
                loop=0, optimize=True, disposal=1)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
