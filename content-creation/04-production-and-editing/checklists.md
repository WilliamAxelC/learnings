# Production & Export Checklists

Print or pin this checklist next to your recording station. Going through these checks prevents the pain of recording an entire 20-minute take only to realize your microphone was muted or your editor font was unreadable.

---

## 1. Pre-Flight Recording Checklist

Run through these checks 5 minutes before pressing Record:

- [ ] **Do Not Disturb**: Phone, laptop, and watch silenced. Slack/Discord notifications closed completely.
- [ ] **Room Acoustics**: Windows closed, fan/AC unit turned off, closet/room doors shut.
- [ ] **Microphone Check**:
  - Distance: 4 to 6 inches from mouth with pop filter aligned.
  - Recording input device confirmed in software (not accidentally using laptop internal mic).
  - Levels test: Vocal peaks hit between -12 dB and -6 dB.
- [ ] **Lighting & Camera**:
  - Camera lens wiped clean with microfiber cloth.
  - Camera placed at eye level.
  - Key light and backlight turned on; exposure and white balance locked.
  - Frame check: Eyes on upper horizontal third; clean background behind subject.
- [ ] **Screen Recording Preparation**:
  - Resolution set to 1920x1080 or 2560x1440 (16:9 aspect ratio).
  - Browser tabs cleaned; bookmarks bar hidden (`Cmd+Shift+B` or `Ctrl+Shift+B`).
  - IDE font scaled up to 18pt or larger (`Cmd + =` or `Ctrl + =`).
  - Terminal history and sensitive environment variables (`.env`, API tokens) cleared.

---

## 2. Editing Quality Gate Checklist

Before rendering your final cut:

- [ ] **Pacing Trim**: All long pauses over 0.4 seconds trimmed in A-roll.
- [ ] **Audio Normalization**: Spoken dialogue normalized to **-14 LUFS** (YouTube standard) with true peaks not exceeding **-1.0 dB**.
- [ ] **Music Balancing**: Background music track attenuated to between **-22 dB and -26 dB** under dialogue.
- [ ] **Subtitles & Captions**: For short-form vertical videos (9:16), captions placed in the safe middle zone (avoiding top icons and bottom UI text overlay).
- [ ] **Visual Clarity**: Code snippets and diagrams verified sharp and legible on mobile screen preview.

---

## 3. Thumbnail Packaging Gate Checklist

Before uploading your custom thumbnail:

- [ ] **3-Element Rule**: No more than 3 distinct visual components in the image.
- [ ] **Text Restraint**: 4 words or fewer on the image. High-contrast sans-serif font.
- [ ] **Non-Duplication**: Thumbnail text complements the video title without repeating it verbatim.
- [ ] **Mobile Shrink Test**: Scaled down to 10% size on screen; image subject and text remain immediately recognizable.
- [ ] **File Specs**: 1920x1080 resolution, PNG or high-quality JPG, under 2MB file size.
