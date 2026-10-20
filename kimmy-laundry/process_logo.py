import os
from collections import deque
from PIL import Image, ImageFilter, ImageEnhance

def process():
    print("Reading logo_exact_source_crop.png...")
    src = Image.open("/home/agent/orca/learnings/logo_exact_source_crop.png").convert("RGBA")
    w, h = src.size

    # BFS Flood fill from edges to identify pure background
    visited = [[False]*w for _ in range(h)]
    queue = deque()
    pixels = src.load()

    def is_white(r, g, b):
        return r >= 232 and g >= 232 and b >= 232

    # Edges
    for x in range(w):
        for y in [0, h - 1]:
            r, g, b, _ = pixels[x, y]
            if is_white(r, g, b) and not visited[y][x]:
                visited[y][x] = True
                queue.append((x, y))

    for y in range(h):
        for x in [0, w - 1]:
            r, g, b, _ = pixels[x, y]
            if is_white(r, g, b) and not visited[y][x]:
                visited[y][x] = True
                queue.append((x, y))

    while queue:
        cx, cy = queue.popleft()
        for dx, dy in [(-1, 0), (1, 0), (0, -1), (0, 1)]:
            nx, ny = cx + dx, cy + dy
            if 0 <= nx < w and 0 <= ny < h and not visited[ny][nx]:
                nr, ng, nb, _ = pixels[nx, ny]
                if is_white(nr, ng, nb):
                    visited[ny][nx] = True
                    queue.append((nx, ny))

    # Gap between legs
    for y in range(104, 112):
        for x in range(78, 85):
            r, g, b, _ = pixels[x, y]
            if r > 220 and g > 220 and b > 220:
                visited[y][x] = True

    # Build alpha map with defringing
    out = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    out_pixels = out.load()

    # Find border pixels of visited
    is_border = [[False]*w for _ in range(h)]
    for y in range(h):
        for x in range(w):
            if not visited[y][x]:
                # check if neighbors visited
                has_visited_neighbor = False
                for dx, dy in [(-1, 0), (1, 0), (0, -1), (0, 1), (-1, -1), (1, 1), (-1, 1), (1, -1)]:
                    nx, ny = cx + dx, cy + dy
                    if 0 <= x + dx < w and 0 <= y + dy < h:
                        if visited[y + dy][x + dx]:
                            has_visited_neighbor = True
                            break
                if has_visited_neighbor:
                    is_border[y][x] = True

    for y in range(h):
        for x in range(w):
            if visited[y][x]:
                out_pixels[x, y] = (0, 0, 0, 0)
            elif is_border[y][x]:
                r, g, b, _ = pixels[x, y]
                br = max(r, g, b)
                # Compute smooth alpha unmatting against white (255,255,255)
                # If pixel is very bright, it was partially blended with white
                if br > 180:
                    a = (255 - br) / 75.0
                    a = max(0.15, min(1.0, a))
                    # unmult
                    ur = int(max(0, min(255, (r - 255 * (1 - a)) / a)))
                    ug = int(max(0, min(255, (g - 255 * (1 - a)) / a)))
                    ub = int(max(0, min(255, (b - 255 * (1 - a)) / a)))
                    out_pixels[x, y] = (ur, ug, ub, int(a * 255))
                else:
                    out_pixels[x, y] = (r, g, b, 255)
            else:
                r, g, b, _ = pixels[x, y]
                out_pixels[x, y] = (r, g, b, 255)

    out.save("/home/agent/orca/learnings/logo_clean_defringed.png")
    print("Saved logo_clean_defringed.png")

    # Now let us generate the 4K upscale!
    # Target size: 4096 x 4160 (or square 4096 x 4096 with balanced padding)
    print("Upscaling to 4K...")
    # Step A: 2x resize with Lanczos
    up2 = out.resize((w * 2, h * 2), Image.Resampling.LANCZOS)
    up2 = up2.filter(ImageFilter.UnsharpMask(radius=1.2, percent=150, threshold=2))

    # Step B: 4x resize with Lanczos
    up4 = up2.resize((w * 4, h * 4), Image.Resampling.LANCZOS)
    up4 = up4.filter(ImageFilter.UnsharpMask(radius=1.5, percent=160, threshold=2))

    # Step C: Final resize to target 4K resolution (4096 x 4096 square with padding)
    target_dim = 4096
    # Keep aspect ratio
    aspect = w / h
    if aspect > 1.0:
        new_w = target_dim - 256
        new_h = int(new_w / aspect)
    else:
        new_h = target_dim - 256
        new_w = int(new_h * aspect)

    up_final = up4.resize((new_w, new_h), Image.Resampling.LANCZOS)
    up_final = up_final.filter(ImageFilter.UnsharpMask(radius=2.0, percent=130, threshold=2))

    # Center in 4096 x 4096 transparent canvas
    master_4k = Image.new("RGBA", (target_dim, target_dim), (0, 0, 0, 0))
    paste_x = (target_dim - new_w) // 2
    paste_y = (target_dim - new_h) // 2
    master_4k.paste(up_final, (paste_x, paste_y), up_final)

    master_path = "/home/agent/orca/learnings/kimmy_logo_4k_transparent.png"
    master_4k.save(master_path, "PNG", optimize=True)
    print(f"Saved master 4K logo to {master_path}: {master_4k.size}")

    # Copy to brain artifact directory as well
    artifact_path = "/home/agent/.gemini/antigravity-cli/brain/e27d16ab-8335-46ae-81ad-c83017a76c7a/kimmy_logo_4k_transparent.png"
    master_4k.save(artifact_path, "PNG", optimize=True)
    print(f"Copied to artifact directory: {artifact_path}")

process()
