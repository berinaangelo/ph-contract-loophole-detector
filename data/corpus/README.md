# Lease knowledge base

`lease_corpus.json` is the retrieval corpus for the lease/rental contract
loophole detector — the source of every citation and excerpt a MEDIUM or LOW
finding shows the user. The LLM never invents a citation; it can only pick
from what's here.

## Schema

Each entry:

| Field | Meaning |
|---|---|
| `id` | Stable, unique, human-readable (`<source>_<short-name>`) |
| `title` | Short plain-language label |
| `source` | `civil_code` \| `ra9653` \| `rules_of_court` \| `clause_pattern` |
| `citation` | The exact citation shown to the user — verbatim, never generated |
| `issue_tags` | Which [severity](../../internal/severity/severity.go) / [checklist](../../internal/checklist/checklist.go) issue(s) this backs |
| `text` | 1-4 sentences, what gets embedded and keyword-indexed. Keep it focused — one idea per entry, not a whole article, for retrieval precision. |

## Sourcing rules

- **No specific case citations (G.R. numbers).** This project can't guarantee
  case-citation accuracy, and a fabricated citation in a legal tool is worse
  than none. `civil_code` / `ra9653` / `rules_of_court` entries only, plus
  `clause_pattern` entries that reason from general principles (Arts. 19, 24,
  1306, 1229) rather than a named case.
- **`clause_pattern` entries are for the MEDIUM tier** — clauses that are
  legal but one-sided. Don't state or imply a `clause_pattern` clause is
  illegal; that's what `severity`'s deterministic HIGH rules are for. Phrase
  these as "legal to write, but..." consistently.
- **Numeric thresholds (RA 9653's caps) are periodically adjusted** by
  DHSUD issuance and depend on the unit's monthly-rent coverage bracket.
  Any entry citing a specific number says so explicitly ("verify current
  coverage") rather than asserting it as settled.
- **One issue tag per concern, reusing the tags already in code** —
  `security_deposit`, `eviction_process`, `repairs_habitability`,
  `rent_increase`, `penalty_interest`, `termination_notice`,
  `renewal_terms`, `dispute_resolution_venue`, `utilities`,
  `subletting_assignment`, `house_rules_use_restrictions`. Adding a new tag
  here means also wiring it into `severity` or `checklist` — an orphaned tag
  never gets found by retrieval's ranking step.

## What's deliberately not here (v1 scope)

- Jurisprudence/case law (see above)
- DTI/DHSUD tenant advisories, HLURB issuances — not needed to ground a
  finding, can be added later if a gap shows up in practice
- Sample/test lease documents — those are ingestion-pipeline test fixtures,
  not corpus content
