---
type: Reference
title: Article schema
description: JSON-LD for editorial pages. Unlocks rich results and gives Google a structured author + publisher + date model.
resource: https://developers.google.com/search/docs/appearance/structured-data/article
tags: [schema, json-ld, article, structured-data]
timestamp: 2026-06-19T08:00:00Z
---

# Article schema

Add `Article` JSON-LD to every editorial page (blog post, long-form
guide, news story). It's the single highest-leverage structured-data
markup for content sites.

## Minimum viable

```html
<script type="application/ld+json">
{
  "@context": "https://schema.org",
  "@type": "Article",
  "headline": "Title tag rules",
  "description": "The HTML title element is the single highest-leverage SEO control on the page.",
  "author": {
    "@type": "Person",
    "name": "Jane Doe",
    "url": "https://yourdomain.com/authors/jane-doe/"
  },
  "publisher": {
    "@type": "Organization",
    "name": "Your Publication",
    "url": "https://yourdomain.com/"
  },
  "datePublished": "2026-06-19T08:00:00Z",
  "dateModified": "2026-06-19T08:00:00Z",
  "mainEntityOfPage": "https://yourdomain.com/articles/title-tag-rules/"
}
</script>
```

## Fields that matter

- **`headline`** — short form of the title, ≤ 110 characters.
- **`author`** — must be a `Person` (or `Organization` only for
  company-attributed pieces). The `url` should point to a real
  author page with bio and other articles.
- **`datePublished` + `dateModified`** — ISO 8601. Honest dates
  matter — fake "last updated" stamps get caught.
- **`mainEntityOfPage`** — the canonical URL for this article. Should
  match the [canonical](/technical/canonical-urls.md) URL exactly.

## Don't

- Don't mark a marketing landing page as `Article`. Google flags this
  as misuse and may strip the rich result across your domain.
- Don't include an image URL pointing to a 404 or to a non-public
  image. Google fetches and validates.

## Sibling for commerce

[Product schema](/markup/schema-product.md) is the equivalent for
e-commerce pages. The two are distinct entity types and should not
appear together on the same page.
