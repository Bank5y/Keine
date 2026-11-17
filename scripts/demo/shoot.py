"""Screenshots of the demo instance (scripts/demo/run.sh), kept in .scratch/shots/
so they outlive the session that took them (PR evidence, design comparison).

One page:

    .scratch/venv/bin/python scripts/demo/shoot.py alice /courses/1/edit 390x844 edit-phone [--full]

A flow (clicks, scrolling, checks): import it from a script of your own.

    sys.path.insert(0, "scripts/demo")
    from shoot import browser, page, shot
    with browser() as b:
        p = page(b, "admin", 1440, 900)
        p.goto(URL + "/admin/users")
        shot(p, "admin-users")

Logins: alice / bob / carol (<name>-password) and admin (admin-password).
Setup, once: python3 -m venv .scratch/venv && .scratch/venv/bin/pip install -r scripts/demo/requirements.txt
             && .scratch/venv/bin/playwright install chromium
"""
import contextlib, os, pathlib, sys
from playwright.sync_api import sync_playwright

URL = os.environ.get("DEMO_URL", "http://127.0.0.1:18080")
SHOTS = pathlib.Path(__file__).resolve().parents[2] / ".scratch" / "shots"
PASSWORDS = {"admin": os.environ.get("KEINE_ADMIN_PASSWORD", "admin-password")}


@contextlib.contextmanager
def browser():
    with sync_playwright() as p:
        b = p.chromium.launch()
        try:
            yield b
        finally:
            b.close()


def page(b, user, width=1440, height=900):
    """A new page of its own context, signed in as user."""
    ctx = b.new_context(viewport={"width": width, "height": height})
    r = ctx.request.post(URL + "/api/session", data={"provider": "password", "username": user, "password": PASSWORDS.get(user, f"{user}-password")})
    if not r.ok:
        sys.exit(f"sign in as {user}: {r.status} (is the demo running? scripts/demo/run.sh)")
    return ctx.new_page()


def shot(p, name, full=False):
    """Saves .scratch/shots/<name>.png and returns its path."""
    SHOTS.mkdir(parents=True, exist_ok=True)
    path = SHOTS / f"{name}.png"
    p.screenshot(path=str(path), full_page=full)
    print(path)
    return path


if __name__ == "__main__":
    args = [a for a in sys.argv[1:] if a != "--full"]
    if len(args) != 4:
        sys.exit(__doc__)
    user, route, size, name = args
    w, h = (int(x) for x in size.split("x"))
    with browser() as b:
        p = page(b, user, w, h)
        p.goto(URL + route)
        p.wait_for_load_state("networkidle")
        shot(p, name, full="--full" in sys.argv)
