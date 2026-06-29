---
type: Playbook
title: Internal linking playbook
description: How to wire your content into a graph Google understands. Anchor text, link depth, hub-and-spoke, and what not to do.
resource: https://developers.google.com/search/docs/crawling-indexing/links-crawlable
tags: [internal-linking, anchor-text, site-structure]
timestamp: 2026-06-19T08:00:00Z
---

# Internal linking playbook

Internal links do three things at once: they distribute PageRank
across your site, they tell Google what each page is about (via
anchor text), and they let users discover related content.

A site without thoughtful internal linking has a thousand orphaned
pages. A site with it has a graph.

## The structural pattern

**Hub and spoke**. Pick one "hub" page per topic. Make sure every
sub-topic page links back to the hub. Link sub-topics to each other
where the content genuinely overlaps.

```
                ┌─ /topic/sub-a ─┐
                │                │
   /topic/  ───►│                │◄─── /topic/sub-c
                │                │
                └─ /topic/sub-b ─┘
```

The hub gets dense PageRank. The sub-pages inherit topical authority
from the hub. The cross-links between sub-pages tell Google these
sub-topics are related.

## Anchor text

The clickable text of a link is *the* strongest signal Google uses to
classify the target page. "Click here" wastes it. "Title tag rules"
links the target to a query.

But variety matters. If 200 pages all link to your hub with the
identical anchor "title tag rules", Google starts to read that as
manipulation. Mix exact-match, partial-match, and natural-language
phrasing. The right ratio is roughly 30 / 40 / 30.

## Depth

Every page should be reachable within 3 clicks from the home page.
Deeper than that and crawl frequency drops; pages get re-indexed less
often and their rankings stale.

If your category pages link to product pages, but products don't link
back to their category, you have a depth problem. Fix by adding the
category back-link in the product template, not by linking everything
from the home page.

## Don't

- Don't link every keyword. Five well-placed links beat fifty.
- Don't link to pages blocked in
  [robots.txt](/technical/robots-and-sitemap.md) — wasted PageRank.
- Don't link to non-[canonical](/technical/canonical-urls.md) URLs.
  Canonical the duplicates, then link only to the canonical.

For the question of *what's worth linking to* in the first place, see
the [content rubric](/content/content-rubric.md).
