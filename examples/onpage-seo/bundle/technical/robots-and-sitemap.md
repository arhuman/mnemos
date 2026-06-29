---
type: Reference
title: robots.txt + sitemap.xml
description: The two files at the root of your domain that tell Google what to crawl, what to skip, and where the URLs live.
resource: https://developers.google.com/search/docs/crawling-indexing/robots/intro
tags: [robots, sitemap, crawling, indexing]
timestamp: 2026-06-19T08:00:00Z
---

# robots.txt + sitemap.xml

Two files. Two distinct jobs.

## robots.txt

`robots.txt` lives at `https://yourdomain.com/robots.txt`. It tells
well-behaved crawlers what they're allowed to fetch. It is **not** a
security mechanism — anyone can ignore it, and the file is publicly
readable.

```
User-agent: *
Allow: /
Disallow: /admin/
Disallow: /api/

Sitemap: https://yourdomain.com/sitemap-index.xml
```

Common mistakes:

- Disallowing `/css/` or `/js/`. Google needs to render the page to
  index it; blocking assets breaks rendering. See
  [Core Web Vitals](/technical/core-web-vitals.md) — Google measures
  rendered performance.
- Disallowing the URL of your [canonical](/technical/canonical-urls.md).
  Google can't verify the canonical and ignores the declaration.
- Using robots.txt to "noindex" a page. robots.txt blocks **crawling**,
  not indexing. To prevent indexing, use a `<meta name="robots"
  content="noindex">` tag or `X-Robots-Tag: noindex` header.

## sitemap.xml

Sitemaps are an explicit invitation: "here is every URL on my site
that I want indexed." Google still discovers pages via links, but
sitemaps speed up discovery and force a re-crawl signal.

```
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://yourdomain.com/articles/title-tag-rules/</loc>
    <lastmod>2026-06-19</lastmod>
  </url>
</urlset>
```

Rules:

- One sitemap caps at 50,000 URLs or 50 MB. Beyond that, partition
  via a sitemap index file.
- Only list **canonical** URLs. Listing both `/page` and `/page/?utm=x`
  wastes crawl budget and confuses canonicalization.
- Keep `<lastmod>` honest. Google deprioritizes sitemaps where
  `<lastmod>` updates on every fetch — that's a "lying sitemap"
  signal.
- Reference the sitemap from robots.txt (see above).

## Together

robots.txt sets the *boundary* of what Google can read. sitemap.xml
declares the *priorities* within that boundary. They're complementary,
not substitutes.
