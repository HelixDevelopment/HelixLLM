#!/usr/bin/env python3
"""
Phase-3 VISION harness — deterministic KNOWN-CONTENT test image generator.

Renders a REAL PNG whose content is unambiguous and fixed, so a grounded
VLM description can be asserted to CONTAIN that content (§11.4.108 runtime
signature) and a golden-BAD wrong-content assertion can be proven to FAIL
(§11.4.107(10)).

Known content (the ground truth the VLM must describe):
  - a large solid RED CIRCLE
  - on a WHITE background
  - with the black text "HELIX" below the circle (OCR ground truth)

A different image would produce a different description, so a PASS here is
grounded in the actual pixels — not "the model returned something".
"""
import sys
from PIL import Image, ImageDraw, ImageFont

W = H = 512
OUT = sys.argv[1] if len(sys.argv) > 1 else "test_image.png"

img = Image.new("RGB", (W, H), (255, 255, 255))  # WHITE background
d = ImageDraw.Draw(img)

# Large solid RED CIRCLE, centered in the upper portion
cx, cy, r = W // 2, 200, 150
d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(220, 20, 20))  # RED

# Black text "HELIX" below the circle (OCR ground truth)
text = "HELIX"
font = None
for cand in (
    "/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    "/usr/share/fonts/DejaVuSans-Bold.ttf",
):
    try:
        font = ImageFont.truetype(cand, 90)
        break
    except OSError:
        continue
if font is None:
    font = ImageFont.load_default()

bbox = d.textbbox((0, 0), text, font=font)
tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
d.text(((W - tw) / 2 - bbox[0], 400 - bbox[1]), text, fill=(0, 0, 0), font=font)

img.save(OUT, "PNG")
print(f"WROTE {OUT}  ({W}x{H})  content: RED CIRCLE on WHITE, black text 'HELIX'")
