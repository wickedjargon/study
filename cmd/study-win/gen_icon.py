#!/usr/bin/env python3
"""Draw the Windows app icon: a cream flashcard with the accent border and a
green check, in the app's light-scheme colors. Emits study.ico (multi-size);
rsrc packs it into rsrc_windows_amd64.syso, which go build links into
study.exe automatically.

    python3 gen_icon.py
    go run github.com/akavel/rsrc@latest -ico study.ico -o rsrc_windows_amd64.syso
"""

from PIL import Image, ImageDraw

CREAM = (0xfb, 0xf1, 0xc7, 255)
TEXT = (0x3c, 0x38, 0x36, 255)
GREEN = (0x79, 0x74, 0x0e, 255)
ACCENT = (0x07, 0x66, 0x78, 255)


def draw(size):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    s = size / 256

    # The card: rounded, cream, accent border.
    m = 20 * s
    d.rounded_rectangle([m, 44 * s, size - m, size - 44 * s],
                        radius=24 * s, fill=CREAM,
                        outline=ACCENT, width=max(1, int(10 * s)))
    # A prompt line and the check: the result screen in miniature.
    d.line([56 * s, 118 * s, 150 * s, 118 * s], fill=TEXT, width=max(1, int(12 * s)))
    d.line([88 * s, 168 * s, 116 * s, 196 * s], fill=GREEN, width=max(1, int(16 * s)))
    d.line([116 * s, 196 * s, 176 * s, 128 * s], fill=GREEN, width=max(1, int(16 * s)))
    return img


sizes = [16, 24, 32, 48, 64, 128, 256]
imgs = [draw(s) for s in sizes]
imgs[-1].save("study.ico", sizes=[(s, s) for s in sizes],
              append_images=imgs[:-1])
print("wrote study.ico")
