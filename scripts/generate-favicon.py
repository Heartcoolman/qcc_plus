#!/usr/bin/env python3
"""
Generate favicon.ico from PNG source image.
Creates multi-resolution ICO file with 16x16, 32x32, 48x48 sizes.
"""

from PIL import Image
import sys
import os

def generate_favicon(input_path, output_path):
    """
    Generate favicon.ico from PNG image.

    Args:
        input_path: Path to source PNG image
        output_path: Path to output ICO file
    """
    try:
        # Open the source image
        img = Image.open(input_path)

        # Convert RGBA to RGB if needed (ICO format requirement)
        if img.mode == 'RGBA':
            # Create white background
            background = Image.new('RGB', img.size, (255, 255, 255))
            # Paste using alpha channel as mask
            background.paste(img, mask=img.split()[3])
            img = background
        elif img.mode != 'RGB':
            img = img.convert('RGB')

        # Generate multiple sizes for better browser compatibility
        sizes = [(16, 16), (32, 32), (48, 48)]

        # Resize images with high quality resampling
        icons = []
        for size in sizes:
            resized = img.resize(size, Image.Resampling.LANCZOS)
            icons.append(resized)

        # Save as ICO with multiple resolutions
        icons[0].save(
            output_path,
            format='ICO',
            sizes=[icon.size for icon in icons],
            append_images=icons[1:]
        )

        print(f"✅ Successfully generated {output_path}")
        print(f"   Sizes included: {', '.join([f'{s[0]}x{s[1]}' for s in sizes])}")

        # Display file size
        file_size = os.path.getsize(output_path)
        print(f"   File size: {file_size:,} bytes ({file_size/1024:.1f} KB)")

        return True

    except Exception as e:
        print(f"❌ Error generating favicon: {e}", file=sys.stderr)
        return False

if __name__ == '__main__':
    if len(sys.argv) != 3:
        print("Usage: python3 generate-favicon.py <input.png> <output.ico>")
        sys.exit(1)

    input_path = sys.argv[1]
    output_path = sys.argv[2]

    if not os.path.exists(input_path):
        print(f"❌ Error: Input file not found: {input_path}", file=sys.stderr)
        sys.exit(1)

    success = generate_favicon(input_path, output_path)
    sys.exit(0 if success else 1)
