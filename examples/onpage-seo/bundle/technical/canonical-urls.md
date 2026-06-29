---
type: Reference
title: Canonical URLs
description: One URL per piece of content. Everything else points to it. How to declare canonicals correctly.
resource: https://developers.google.com/search/docs/crawling-indexing/consolidate-duplicate-urls
tags: [canonical, duplicate-content, indexing]
timestamp: 2026-06-19T08:00:00Z
---

# Canonical URLs

Every public URL on your site falls into one of two roles: it's the
canonical (the version you want indexed) or it isn't. The non-canonical
versions need to say so — either via a `<link rel="canonical">` tag
pointing to the canonical, or via a 301 redirect.

## When you need this

- Your CMS adds tracking parameters (`?utm_source=…`) to URLs.
- Pagination creates `/blog/`, `/blog/page/2/`, `/blog/page/3/` all
  with the same first-screen content.
- Filtering on a listing page creates dozens of facet URLs with overlapping
  content.
- HTTP and HTTPS, or `www` and apex, both serve the same page.
- Trailing-slash variants (`/page` vs `/page/`) both serve the same page.

## How to declare

The `<link rel="canonical" href="...">` tag in `<head>` tells Google
which URL is the authoritative one. Use the **full absolute URL**.
Always self-canonicalize: every canonical page declares itself.

```
<link rel="canonical" href="https://example.com/articles/title-tag-rules/">
```

If you use redirects instead — and you should, for the http/https and
www/apex cases — return a 301 (permanent), not a 302. Google
consolidates ranking signals across 301s within a few weeks.

## Common bugs

- Canonical pointing to a 404. Audit for this monthly.
- Canonical pointing across protocols (https page canonicalizing to
  http). Always serve canonicals on the same protocol.
- Mismatched trailing slashes between the canonical tag and the
  served URL.
- Canonical to a URL blocked by [robots.txt](/technical/robots-and-sitemap.md).
  Google can't crawl the canonical to verify, so it ignores the
  declaration.

## After canonicalization

Once you've consolidated, your [sitemap](/technical/robots-and-sitemap.md)
should list only the canonical URLs. Non-canonical URLs should not
appear in the sitemap at all.
