# PLAN

## One-sentence description

A user pastes or uploads a Philippine residential lease contract and, within
seconds, sees its clauses ranked by real risk (HIGH/MEDIUM/LOW), each with a
plain-language reason, a real citation, and a suggested action — never a
diagnosis of "sign or don't," always paired with "talk to a lawyer."

("Real citation" applies to severity and RAG findings, both grounded in
the knowledge base. `checklist`'s missing-clause findings are best-practice
recommendations, not statute violations, so they carry no citation — by
design, not by omission.)

## Primary flow

1. **Input.** User pastes contract text, or uploads a `.docx`/`.pdf`
   ([internal/extract](internal/extract/extract.go)). If the file can't
   produce usable text (a scanned/image-only PDF has no text layer), they
   see that plainly — "we couldn't read this file" — never a silent
   analysis of nothing.
2. **Language gate.** Non-English input gets a fixed, never-regenerated
   rejection string ([internal/language.UnsupportedMessage](internal/language/language.go))
   — v1 is English-only; Taglish is a known real-world case, deliberately
   deferred (see Cut).
3. **Loading.** A short, legible staged wait (mirrors the reference app's
   "checking → searching → generating" pattern) — not a spinner with no
   information.
4. **Results.** Three findings sources run per contract — deterministic
   severity (per clause), deterministic checklist (whole-document gap
   check), and RAG+LLM (per clause, floor-gated) — merged into one list via
   the shared shape below, always rendered in this fixed order:
   - **Scorecard first** — "2 HIGH, 4 MEDIUM, 1 LOW" — before any clause
     detail. Peak-End + Hick's Law: lead with the number that tells them
     how worried to be, let them decide whether to read further.
   - **"Looks clean" state when the scorecard is 0/0/0.** Checklist gap
     findings are near-guaranteed on any real contract, so an all-zero
     result should be rare — but when it happens it's a legitimate,
     reassuring answer ("no issues found — still worth a lawyer's look
     before signing"), not the same screen as a failed/unreadable upload.
     Distinct from "no match" below.
   - **Findings sorted HIGH → MEDIUM → LOW**, never by document position
     and never interleaved. HIGH is the god moment (see below) — it must
     be the first thing read, not found by scrolling.
   - **Each finding quotes its matched clause text verbatim**, next to the
     citation and explanation — this is what makes a finding feel like it
     read *their* document instead of giving generic advice.
   - **LOW "missing clause" gaps** ([internal/checklist](internal/checklist/checklist.go))
     shown last, clearly distinguished from clause-specific findings —
     they're about what's absent, not what's wrong. Checklist itself only
     runs once `LooksLikeLease` passes (see Design decisions) — that gate
     stops it from reporting "missing clauses" on a document that was
     never a lease, but doesn't affect severity or RAG findings, which
     still show normally either way. Beyond that gate, checklist still
     assumes the *full* contract was submitted, not one clause; the input screen's
     placeholder text says so explicitly ("paste the full lease, not just
     one clause") — cheaper than building a "is this a full contract"
     detector, and accurate enough since a short-but-lease-shaped paste
     still won't false-fail, it'll just correctly flag everything past
     what it actually covers as missing.
   - **The disclaimer renders on every results state**, not once in a
     footer — same fixed string every time, never re-typed per screen.
5. **No match** is decided *after* running everything, not upfront by one
   heuristic. Severity always runs (its phrase/regex rules are specific
   enough not to false-positive on off-topic text), checklist runs only if
   `checklist.LooksLikeLease` passes (see Design decisions — that gate is
   checklist's problem to solve, not the whole pipeline's), and RAG always
   runs (the relevance floor already handles irrelevance on its own). "No
   match" fires only if all three come back empty — e.g. text with no
   lease-shaped content and no clause matching any known issue at all.
   Pasting one real clause by itself, with no surrounding lease-shaped
   document, still gets a real severity/RAG result even when
   `LooksLikeLease` fails and checklist gets skipped for that request —
   that's a valid, common input shape (checking one suspicious clause a
   landlord sent), not a rejection case.

## Design decisions this patch adds

These aren't UI polish — they're load-bearing decisions the rest of the
plan assumed were already settled. Made explicit now, before `retrieve`/
`generate`/`server` get built, because they're cheap to change on paper and
expensive to change in code.

- **`checklist.LooksLikeLease` gates only whether checklist runs — nothing
  else.** Checklist's own check (keyword *absence*) structurally can't
  distinguish "this lease is missing a clause" from "this text isn't a
  lease" — both look identical to an absence check, so it needs a cheap
  presence gate (≥2 of `landlord`/`lessor`, `tenant`/`lessee`, `lease`,
  `rent`) before it runs at all. Severity and RAG don't share that failure
  mode — severity's phrase rules are precise enough not to false-positive
  on off-topic text, and RAG's relevance floor already handles irrelevance
  — so `internal/server` runs both regardless of `LooksLikeLease`, and
  decides "no match" from the combined result of all three (Primary flow,
  step 5), never from `LooksLikeLease` alone. Gating the whole response on
  it would silently break a real, common input shape — pasting one
  suspicious clause with no surrounding lease-shaped document — which
  several of `severity`'s own test fixtures happen to look exactly like.
- **One shared `Finding` shape.** `severity.Finding`, `checklist.Finding`,
  and `generate`'s not-yet-built output are three different Go structs
  today. Before results can be "sorted HIGH → MEDIUM → LOW" as one list,
  `internal/server` needs to map all three into one common shape
  (severity, issue, explanation, citation/statute, clause text, action) —
  this is `internal/server`'s job specifically, not something the three
  source packages should be contorted to share directly (they stay
  independently testable without knowing about each other, same as today).
- **`generate` batches per contract, capped, keyed by (clause, issue) —
  not by issue name alone.** The reference app keyed its structured-output
  schema by *condition name* (one enum entry per unique condition), safe
  there because `fuse.RankConditions` deduplicated within one query — one
  call, no repeats possible. This project's `fuse.RankIssues` runs *per
  clause*, so uniqueness only holds within one clause's ranking, not
  across the whole contract: with 11 issue tags spread across a typical
  15-20 clause lease, two different clauses independently surfacing the
  same tag (e.g. a landlord-termination clause and a tenant-termination
  clause both tagged `termination_notice`) is likely, not an edge case.
  Reusing the reference app's issue-name-keyed schema would silently drop
  or blend one of those two real, distinct findings. So each candidate in
  the batch schema needs its own id keyed by (clause index, issue tag),
  not the issue tag alone — the `Finding` shape already carries clause
  text to disambiguate two same-issue findings once they exist; this is
  what keeps them from colliding earlier, inside the LLM call itself.
  Separately: the reference app verified its 5-condition batch live
  against qwen2.5:7b, not just assumed it — "batch per contract" alone
  doesn't inherit that verification, since a real lease's surviving
  candidates could plausibly be well past 5, and a much larger
  enum-constrained schema in one call is a real, different risk on a 7B
  local model (context length, structured-output reliability). So: cap
  batches at up to 10 candidates per call, chunking into a small number of
  sequential calls beyond that (still ~2-3 calls for a typical lease,
  nowhere near per-clause) — and re-verify live at the chosen cap before
  trusting it. This still satisfies "single synchronous response" below:
  the *client* gets one response; `generate` may issue a few sequential
  calls internally to produce it.

## Usability decisions — in

| Decision | Why (which gate it passes) |
|---|---|
| Scorecard before clause detail | Demo test — the whole point is demoable in the first 2 seconds on screen |
| HIGH → MEDIUM → LOW fixed ordering | God-moment test — the highest-value finding must never require scrolling to find |
| Quoted clause text on every finding | "Why does this exist" — without it, findings read as generic advice, not analysis of *this* document |
| Clear "couldn't read this file" state | The `extract` package already fails loudly on this (`ErrNoExtractableText`) — a UI that doesn't surface it would waste that guard rail |
| Disclaimer on every results screen | One-narrative test — trust framing can't be something the user might scroll past once and forget |
| Single synchronous response (see Cut: streaming) | Keeps the primary flow one request/response, not a partial-state protocol the client has to reason about — **only holds if `generate` batches per contract** (see Design decisions above); per-clause calls would make this response too slow to be one screen |

## Cut

- **Inline document highlighting** (rendering the uploaded doc with
  highlighted spans) — fails "why does this exist": quoting the matched
  clause text in each finding card already delivers the same trust signal
  for far less UI work. Revisit only if user testing shows the card-based
  version isn't enough.
- **Confidence indicator separate from severity** — fails one-narrative:
  severity already *is* the confidence signal (HIGH = deterministic rule
  match, MEDIUM/LOW = RAG-grounded past the relevance floor). A second
  dial crossed with severity is a 3×3 grid to parse, not a clearer signal.
- **Streaming HIGH findings before MEDIUM/LOW finish** — fails
  "why does this exist" once ordering (above) already puts HIGH first in a
  single response. Streaming would chase the same perceived-speed goal
  through a materially more complex partial-response protocol.
- **Contract-type presets** (employment, freelance, NDA, loan) — out of
  scope; v1 is lease/rental only, decided earlier.
- **Taglish / non-English input** — deferred; the language gate rejects it
  cleanly rather than half-supporting it.
- **Export-as-PDF / report generation** — the browser's built-in
  print-to-PDF already covers "bring this to a lawyer" for free; custom
  export code isn't earning its place yet.
- **Feedback loop** (flag a wrong finding) — "someone might want it," no
  persistence layer exists to act on it yet.
- **OCR for scanned PDFs** — already cut when `extract` was built; a
  scanned PDF fails cleanly instead of silently.

## Build status

| Piece | Status |
|---|---|
| `internal/language`, `internal/clause`, `internal/severity`, `internal/fuse` | Done, tested |
| `internal/checklist` (topic gap check, incl. `LooksLikeLease`) | Done, tested |
| `internal/extract` (DOCX/PDF → text) | Done, tested |
| `data/corpus/lease_corpus.json` (knowledge base) | Done — 23 entries across all 11 issue tags |
| `internal/retrieve` (embed → hybrid search → fuse → floor) | Not started |
| `internal/generate` (LLM explanation + suggested action, **batched in capped groups keyed by clause+issue, verify size live** — see Design decisions above) | Not started |
| `internal/server` + `web/` (HTTP layer, upload endpoint, **maps severity/checklist/generate findings into one shared shape**, UI implementing the flow above) | Not started |
| `cmd/ingest` (loads the corpus into Qdrant + Bleve) | Not started |
| Old medical-app leftovers (`internal/store`, `internal/embed`, `internal/generate`, `internal/server`, `internal/redflag`, `cmd/*`, `web/*`) | Untouched, still broken — superseded piece by piece as the above gets built |
