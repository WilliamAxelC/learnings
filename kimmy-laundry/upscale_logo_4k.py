import sys
import numpy as np
from PIL import Image, ImageFilter
from collections import deque

def main():
    print("Loading original exact logo crop...")
    src = Image.open("/home/agent/orca/learnings/logo_exact_source_crop.png").convert("RGBA")
    w, h = src.size
    print(f"Source dimensions: {w}x{h}")

    # Step 1: Detect background on the source image
    # The perimeter is pure white (RGB > 240)
    pixels = src.load()
    bg_mask = np.zeros((h, w), dtype=bool)
    
    # BFS from borders
    queue = deque()
    for x in range(w):
        for y in [0, h - 1]:
            r, g, b, _ = pixels[x, y]
            if r > 232 and g > 232 and b > 232 and not bg_mask[y, x]:
                bg_mask[y, x] = True
                queue.append((x, y))
                
    for y in range(h):
        for x in [0, w - 1]:
            r, g, b, _ = pixels[x, y]
            if r > 232 and g > 232 and b > 232 and not bg_mask[y, x]:
                bg_mask[y, x] = True
                queue.append((x, y))

    while queue:
        cx, cy = queue.popleft()
        for dx, dy in [(-1, 0), (1, 0), (0, -1), (0, 1)]:
            nx, ny = cx + dx, cy + dy
            if 0 <= nx < w and 0 <= ny < h and not bg_mask[ny, nx]:
                r, g, b, _ = pixels[nx, ny]
                if r > 232 and g > 232 and b > 232:
                    bg_mask[ny, nx] = True
                    queue.append((nx, ny))

    # Also mark negative space between legs as background (x: 78..84, y: 104..111)
    for y in range(104, 112):
        for x in range(78, 85):
            r, g, b, _ = pixels[x, y]
            if r > 220 and g > 220 and b > 220:
                bg_mask[y, x] = True

    print(f"Background mask created: {np.sum(bg_mask)} background pixels")

    # Step 2: Unmult boundary pixels on the source image to eliminate white halos
    # Create float RGBA array
    img_np = np.array(src, dtype=np.float32)
    rgb = img_np[:, :, :3]
    alpha = np.ones((h, w), dtype=np.float32) * 255.0
    alpha[bg_mask] = 0.0

    # Find boundary pixels (foreground pixels adjacent to background)
    from scipy.ndimage import binary_dilation
    dilated_bg = binary_dilation(bg_mask, structure=np.ones((3,3)))
    edge_zone = dilated_bg & (~bg_mask)

    # In the edge zone, compute soft alpha based on brightness
    # Since background was white (255,255,255), pixel = F*a + 255*(1-a)
    # So a = (255 - max(r,g,b)) / (255 - min_F)
    for y in range(h):
        for x in range(w):
            if edge_zone[y, x]:
                r, g, b = rgb[y, x]
                # Brightness
                br = max(r, g, b)
                if br > 200:
                    a = (255.0 - br) / 55.0  # soft fade from 200 (a=1) to 255 (a=0)
                    a = max(0.0, min(1.0, a))
                    alpha[y, x] = a * 255.0
                    # Recover foreground color by unmulting white
                    if a > 0.05:
                        for c in range(3):
                            rgb[y, x, c] = np.clip((rgb[y, x, c] - 255.0 * (1.0 - a)) / a, 0, 255)

    clean_rgba = np.dstack([rgb, alpha]).astype(np.uint8)
    clean_src = Image.fromarray(clean_rgba, "RGBA")
    clean_src.save("/home/agent/orca/learnings/logo_clean_unmult.png")
    print("Saved logo_clean_unmult.png")

if __name__ == "__main__":
    main()
