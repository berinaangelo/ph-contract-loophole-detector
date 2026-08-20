# Mockups

`index.html` — self-contained, no build step or server needed. Open it
directly in a browser.

It's a clickable prototype of [PLAN.md](../../PLAN.md)'s primary flow, using
realistic sample findings drawn from the actual `severity`/`checklist`/
corpus content already built, not placeholder text:

- **Input** → **Loading** (staged) → **Results** is a real click-through
  (the "Analyze contract" button drives it, same as the eventual product).
- The dark strip at the top (**MOCKUP REVIEW**) is *not* product UI — it's a
  jump-to-any-screen nav for reviewing states that aren't reachable by
  clicking through with the sample data (looks-clean, no-match, file error).
- Each screen has a caption below it noting which PLAN.md decision it's
  demonstrating.

This is a design artifact for review before `internal/server`/`web/` get
built — not the real implementation.
