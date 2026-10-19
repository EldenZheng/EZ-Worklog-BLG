"""Finish the README media from the captures readme_demo_test.go writes.

    python docs/make_readme_media.py docs/screenshots <frames dir> docs/demo.gif

Crops the logging popup stills to the popup, draws the callout on the minutes
field, and stitches the popup walkthrough into the GIF with a drawn pointer —
the off-screen renderer has no mouse of its own.
"""
import json
import sys
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

# The popup is 80% of the 1680x1000 window, centred; the calendar drops a little
# past its bottom edge, so the crop leaves room for it.
CROP = (160, 92, 1520, 948)
GIF_SCALE = 0.85
GOLD = (232, 182, 58)


def font(size):
    for name in ("segoeuib.ttf", "DejaVuSans-Bold.ttf", "Arial Bold.ttf"):
        try:
            return ImageFont.truetype(name, size)
        except OSError:
            pass
    return ImageFont.load_default()


def callout(im, box):
    """A gold ring round the field, its label in a pill just above it."""
    d = ImageDraw.Draw(im)
    pad = 6
    x0, y0 = box["X"] - pad, box["Y"] - pad
    x1, y1 = box["X"] + box["W"] + pad, box["Y"] + box["H"] + pad
    d.rounded_rectangle((x0, y0, x1, y1), radius=8, outline=GOLD, width=4)
    f = font(17)
    tw = d.textlength(box["Label"], font=f)
    # Hung off the right end, clear of the field's own caption above its left.
    lx, ly = x1 - tw - 24, y0 - 72
    d.rounded_rectangle((lx, ly, lx + tw + 24, ly + 32), radius=16, fill=GOLD)
    d.text((lx + 12, ly + 6), box["Label"], font=f, fill=(24, 24, 24))
    d.line((x1 - 20, ly + 32, x1 - 20, y0), fill=GOLD, width=3)


def pointer(im, at, click):
    """An arrow pointer with its tip at `at`, and a ring under it on a click."""
    d = ImageDraw.Draw(im)
    x, y = at
    if click:
        d.ellipse((x - 18, y - 18, x + 18, y + 18), outline=GOLD, width=4)
    shape = [(0, 0), (0, 26), (7, 20), (12, 31), (17, 29), (12, 18), (21, 18)]
    pts = [(x + px, y + py) for px, py in shape]
    d.polygon(pts, fill=(255, 255, 255), outline=(0, 0, 0))
    d.line(pts + [pts[0]], fill=(0, 0, 0), width=2)


def stills(shots):
    popup = Image.open(shots / "08-log-popup.png").convert("RGB")
    for box in json.loads((shots / "08-log-popup.json").read_text()):
        callout(popup, box)
    popup.crop(CROP).save(shots / "08-log-popup.png")
    (shots / "08-log-popup.json").unlink()
    cal = Image.open(shots / "09-date-calendar.png").convert("RGB")
    cal.crop(CROP).save(shots / "09-date-calendar.png")


def gif(frames_dir, dst):
    plan = json.loads((frames_dir / "frames.json").read_text())
    size = (round((CROP[2] - CROP[0]) * GIF_SCALE), round((CROP[3] - CROP[1]) * GIF_SCALE))
    cache, seq, durations = {}, [], []
    for f in plan:
        if f["file"] not in cache:
            cache[f["file"]] = Image.open(frames_dir / f["file"]).convert("RGB")
        im = cache[f["file"]].copy()
        if f.get("cursor"):
            pointer(im, f["cursor"], f.get("click", False))
        seq.append(im.crop(CROP).resize(size, Image.LANCZOS))
        durations.append(f["ms"])

    # One palette for every frame, taken from all the distinct captures at once,
    # so the dark surfaces do not shimmer between frames.
    sheet = Image.new("RGB", (size[0], size[1] * len(cache)))
    for i, im in enumerate(cache.values()):
        pointer(im, (40, 40), True)
        sheet.paste(im.crop(CROP).resize(size), (0, i * size[1]))
    pal = sheet.quantize(colors=255, method=Image.Quantize.MEDIANCUT)
    out = [im.quantize(palette=pal, dither=Image.Dither.NONE) for im in seq]
    out[0].save(dst, save_all=True, append_images=out[1:], duration=durations,
                loop=0, optimize=True, disposal=1)


if __name__ == "__main__":
    stills(Path(sys.argv[1]))
    gif(Path(sys.argv[2]), sys.argv[3])
