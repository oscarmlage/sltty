#!/usr/bin/env python3
"""Record docs/assets/demo/*.tape as GIFs, in a real kitty window.

vhs renders in xterm.js, which cannot show kitty graphics, so pictures and
custom emoji would be missing. This driver runs sltty in kitty, types the
tape's keys into it through kitty's remote control and films the window
with ffmpeg (macOS screen capture).

Tape syntax (a subset of vhs): Output, Hide/Show (what is typed between them
is ignored: the driver starts sltty itself), Type, Enter, Escape, Tab,
Backspace, Down/Up/Left/Right, Ctrl+<letter>, Sleep. Lines starting with # are comments.

Needs: macOS, kitty, ffmpeg and Screen Recording permission for the terminal
running this. Do not touch the keyboard or move windows while it records.

    scripts/record-demo.py docs/assets/demo/threads.tape [...]
"""
import json, os, re, shutil, subprocess, sys, tempfile, time

W, H, FONT, FPS, GIF_W = 1600, 900, 15, 12, 1000
SOCK = "/tmp/sltty-record.sock"
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
KEYS = {"Enter": "\r", "Escape": "\x1b", "Tab": "\t", "Space": " ",
        "Backspace": "\x7f", "Down": "\x1b[B", "Up": "\x1b[A", "Right": "\x1b[C", "Left": "\x1b[D"}


def parse_dur(s):
    return float(s[:-2]) / 1000 if s.endswith("ms") else float(s.rstrip("s"))


def parse(path):
    out, steps, hidden = None, [], False
    for line in open(path):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        cmd, _, arg = line.partition(" ")
        if cmd == "Output":
            out = arg
        elif cmd == "Hide":
            hidden = True
        elif cmd == "Show":
            hidden = False
        elif hidden or cmd in ("Set", "Env", "Source"):
            continue
        elif cmd == "Type":
            steps.append(("type", json.loads(arg)))
        elif cmd == "Sleep":
            steps.append(("sleep", parse_dur(arg)))
        elif cmd in KEYS:
            steps.append(("key", KEYS[cmd]))
        elif m := re.fullmatch(r"Ctrl\+([A-Za-z])", cmd):
            steps.append(("key", chr(ord(m[1].upper()) - 64)))
        else:
            sys.exit(f"{path}: unsupported line: {line}")
    return out, steps


def send(text):
    subprocess.run(["kitty", "@", "--to", "unix:" + SOCK, "send-text", "--", text],
                   check=True, stdout=subprocess.DEVNULL)


def bounds(title):
    js = f'''ObjC.import("CoreGraphics");
var l=ObjC.deepUnwrap(ObjC.castRefToObject($.CGWindowListCopyWindowInfo(1,0)));
var w=l.filter(w=>w.kCGWindowName==="{title}")[0];
w?JSON.stringify(w.kCGWindowBounds):""'''
    r = subprocess.run(["osascript", "-l", "JavaScript", "-e", js], capture_output=True, text=True).stdout.strip()
    return json.loads(r) if r else None


def record(tape):
    out, steps = parse(tape)
    out = os.path.join(ROOT, out)
    title = "sltty-record"
    os.makedirs("/tmp/home/demo", exist_ok=True)
    if os.path.exists(SOCK):
        os.remove(SOCK)
    kitty = subprocess.Popen(
        ["kitty", "--title", title, "--listen-on", "unix:" + SOCK,
         "-o", "allow_remote_control=socket-only", "-o", "remember_window_size=no",
         "-o", f"initial_window_width={W}", "-o", f"initial_window_height={H}",
         "-o", f"font_size={FONT}", "-o", "hide_window_decorations=titlebar-only",
         "-o", "confirm_os_window_close=0", "-o", "macos_quit_when_last_window_closed=yes",
         "-o", "window_padding_width=6",
         # a fake home: no personal config or state, no real user name on screen
         "env", "HOME=/tmp/home/demo", "XDG_CONFIG_HOME=/tmp/home/demo/.config", "XDG_CACHE_HOME=/tmp/home/demo/.cache",
         "./bin/sltty", "-demo"], cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        b = None
        for _ in range(50):
            time.sleep(0.2)
            b = bounds(title)
            if b:
                break
        if not b:
            sys.exit("kitty window not found")
        # wait until the window has stopped moving and sltty is up
        for _ in range(20):
            time.sleep(1.0)
            b2 = bounds(title)
            if b2 == b:
                break
            b = b2
        time.sleep(1.5)
        x, y, w, h = (int(b[k]) for k in ("X", "Y", "Width", "Height"))
        tmp = tempfile.mkdtemp()
        raw = os.path.join(tmp, "cap.mp4")
        ff = subprocess.Popen(
            ["ffmpeg", "-v", "error", "-y", "-f", "avfoundation", "-framerate", "30", "-capture_cursor", "0",
             "-i", "5:none", "-vf", f"crop={w}:{h}:{x}:{y}", "-c:v", "libx264", "-preset", "ultrafast",
             "-crf", "16", "-pix_fmt", "yuv420p", raw],
            stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(1.0)
        for kind, v in steps:
            if kind == "sleep":
                time.sleep(v)
            elif kind == "key":
                send(v)
                # A lone ESC followed at once by another key reads as Alt+key.
                time.sleep(0.25 if v == "\x1b" else 0.1)
            else:
                for ch in v:
                    send(ch)
                    time.sleep(0.045)
        time.sleep(0.8)
        ff.stdin.write(b"q"); ff.stdin.flush(); ff.wait(timeout=60)
        pal = os.path.join(tmp, "pal.png")
        vf = f"fps={FPS},scale={GIF_W}:-1:flags=lanczos"
        subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", raw, "-vf", vf + ",palettegen=stats_mode=diff", pal], check=True)
        subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", raw, "-i", pal, "-lavfi",
                        vf + "[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle", out], check=True)
        shutil.rmtree(tmp)
        print(f"{out} ({os.path.getsize(out) // 1024} KiB)")
    finally:
        kitty.terminate()
        try:
            kitty.wait(timeout=5)
        except subprocess.TimeoutExpired:
            kitty.kill()


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    for t in sys.argv[1:]:
        record(t)
