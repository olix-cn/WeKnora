"""Import an AethCraft brand kit, trimming only blank wordmark canvas.

Usage: python scripts/sync-brand-assets.py <brand-kit-directory>
Requires Pillow. Source images and the style guide are kept in docs/branding.
"""
from pathlib import Path
import argparse
import base64
import io
import re
import shutil
from PIL import Image, ImageChops

ROOT = Path(__file__).resolve().parents[1]

def save(image, name, **kwargs):
    path = ROOT / name
    path.parent.mkdir(parents=True, exist_ok=True)
    image.save(path, **kwargs)

def wordmark(path, dark=False):
    image = Image.open(path).convert('RGBA')
    if dark:
        rgb = image.convert('RGB')
        background = Image.new('RGB', image.size, rgb.getpixel((0, 0)))
        mask = ImageChops.difference(rgb, background).convert('L').point(lambda v: 255 if v > 35 else 0)
        box = mask.getbbox()
        image = image.crop(box)
        # The supplied on-dark variant has an opaque black canvas.
        pixels = image.load()
        for y in range(image.height):
            for x in range(image.width):
                r, g, b, a = pixels[x, y]
                pixels[x, y] = (r, g, b, max(r, g, b))
    else:
        image = image.crop(image.getchannel('A').getbbox())
    # Keep a quarter-height clear space as specified by the supplied guide.
    pad = round(image.height / 4)
    canvas = Image.new('RGBA', (image.width + 2 * pad, image.height + 2 * pad))
    canvas.alpha_composite(image, (pad, pad))
    return canvas

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('brand_kit', type=Path)
    kit = parser.parse_args().brand_kit
    archive = ROOT / 'docs/branding/aethcraft'
    archive.mkdir(parents=True, exist_ok=True)
    if kit.resolve() != archive.resolve():
        for folder in ('variants', 'icons', 'social'):
            shutil.copytree(kit / folder, archive / folder, dirs_exist_ok=True)
        for name in ('brand-colors.json', 'brand-colors.css', 'style-guide.pdf', 'README.txt'):
            shutil.copy2(kit / name, archive / name)
    light = wordmark(kit / 'variants/logo-transparent.png')
    dark = wordmark(kit / 'variants/logo-on-dark.png', dark=True)
    for prefix in ('frontend/public/brand', 'website-docs/homepage/public/docs/_home/brand'):
        save(light, f'{prefix}/aethcraft-light.png')
        save(dark, f'{prefix}/aethcraft-dark.png')
    # Existing asset paths remain valid for integrations and older documentation.
    for name in ('frontend/src/assets/img/weknora.png', 'docs/images/logo.png', 'website-docs/homepage/public/docs/_home/brand/weknora-original.png', 'misc/intro-video/assets/logo-light.png', 'docs/images/readme/src/wordmark-light.png'):
        save(light, name)
    for name in ('misc/intro-video/assets/logo-dark.png', 'docs/images/readme/src/wordmark-dark.png'):
        save(dark, name)
    icon = Image.open(kit / 'icons/app-icon-1024.png').convert('RGBA')
    favicon = Image.open(kit / 'icons/favicon-32.png').convert('RGBA')
    for name in ('frontend/public/favicon.ico', 'website-docs/public/favicon.ico'):
        save(favicon, name, format='ICO', sizes=[(16, 16), (32, 32)])
    save(icon.resize((180, 180), Image.Resampling.LANCZOS), 'frontend/public/apple-touch-icon.png')
    save(icon, 'cmd/desktop/build/appicon.png')
    save(icon, 'frontend/public/brand/app-icon.png')
    social = next((kit / 'social').glob('og-1200x630*.png'))
    for target in ('frontend/public/brand/og.png', 'website-docs/homepage/public/docs/_home/brand/og.png'):
        shutil.copy2(social, ROOT / target)
    # README SVGs embed their wordmarks, so updating only the source PNG is insufficient.
    for path in (ROOT / 'docs/images/readme').glob('*.svg'):
        image = dark if path.stem.endswith('-dark') else light
        buffer = io.BytesIO()
        image.save(buffer, format='PNG', optimize=True)
        data = 'data:image/png;base64,' + base64.b64encode(buffer.getvalue()).decode()
        text = path.read_text(encoding='utf-8')
        updated = re.sub(r'data:image/png;base64,[A-Za-z0-9+/=]+', lambda _: data, text)
        if updated != text:
            path.write_bytes(updated.encode('utf-8'))
    print(f'AethCraft assets imported; wordmark {light.width}x{light.height}.')

if __name__ == '__main__':
    main()
