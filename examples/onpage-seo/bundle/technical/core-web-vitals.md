---
type: Reference
title: Core Web Vitals
description: Three performance metrics Google measures from the field. The targets, what they catch, and how to fix the common failures.
resource: https://web.dev/vitals/
tags: [performance, cwv, lcp, inp, cls]
timestamp: 2026-06-19T08:00:00Z
---

# Core Web Vitals

Three field-measured performance metrics Google folds into ranking.
The "field" qualifier matters: these are measured on real users via
the Chrome User Experience Report (CrUX), not on your own dev machine.

## The three

| Metric | What it measures | Target (good) |
|---|---|---|
| **LCP** — Largest Contentful Paint | When the biggest above-the-fold element finishes loading. | < 2.5 s |
| **INP** — Interaction to Next Paint | Worst-case responsiveness of all user interactions on the page. | < 200 ms |
| **CLS** — Cumulative Layout Shift | How much the layout jumps around after first paint. | < 0.1 |

The threshold for "good" is the **75th percentile** of your real
users. Median-passing is not enough.

## Common failures

**LCP too slow.** Usually a hero image without `fetchpriority="high"`
or served from a CDN with a long TTFB. Sometimes a render-blocking
script in `<head>`.

**INP regressions.** Heavy JS bundle parsing on the main thread, or a
synchronous `localStorage` access in a frequently-fired event handler.
React with a single large component tree often hits INP after
hydration.

**CLS spikes.** Images without explicit `width`/`height`, ads
injected into the layout, web fonts loading without a fallback metric
match (use `size-adjust` in `@font-face`).

## How Google uses this

CWV is a "tiebreaker" ranking factor for query-page pairs where the
content quality is otherwise comparable. It will not lift a thin page
above a deep one. But for two pages with similar relevance, the one
with passing CWV wins more often than not.

It also feeds the "Page Experience" report in Search Console — pages
flagged there are deprioritized for some rich-result formats.

For the indexing decision itself, see
[canonical URLs](/technical/canonical-urls.md) and
[robots and sitemap](/technical/robots-and-sitemap.md) — CWV doesn't
matter if the page isn't indexed in the first place.
