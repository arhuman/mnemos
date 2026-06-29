---
type: Reference
title: Product schema
description: JSON-LD for product pages. Earns price, rating, and stock pills in the SERP.
resource: https://developers.google.com/search/docs/appearance/structured-data/product
tags: [schema, json-ld, product, structured-data, ecommerce]
timestamp: 2026-06-19T08:00:00Z
---

# Product schema

`Product` JSON-LD is the structured-data markup that lets Google show
**price, rating, and stock** directly under the search result. For
e-commerce sites it's worth more than any single content optimization.

## Minimum viable

```html
<script type="application/ld+json">
{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "Mechanical Keyboard, Brown Switches",
  "image": "https://yourshop.com/images/kb-brown.jpg",
  "description": "65% layout, hot-swap, USB-C.",
  "sku": "KB-BROWN-65",
  "brand": { "@type": "Brand", "name": "YourShop" },
  "offers": {
    "@type": "Offer",
    "url": "https://yourshop.com/products/kb-brown/",
    "priceCurrency": "USD",
    "price": "129.00",
    "availability": "https://schema.org/InStock",
    "itemCondition": "https://schema.org/NewCondition"
  },
  "aggregateRating": {
    "@type": "AggregateRating",
    "ratingValue": "4.6",
    "reviewCount": "187"
  }
}
</script>
```

## Fields Google requires

- `name` (the product name)
- `image` (a public, fetchable image URL)
- At least one of: `offers`, `review`, or `aggregateRating`

For the rich-result *price pill* specifically, `offers.price` and
`offers.priceCurrency` are mandatory.

## Stock + price freshness

Google re-crawls product pages frequently — daily for high-traffic
SKUs. The structured data has to stay in sync with the page body. If
your JSON-LD says `InStock` but the buy button says "sold out", Google
strips the rich result for that page until the next crawl. Build the
JSON-LD from the same data source as the visible UI.

## Don't

- Don't mark up a category page as `Product`. The category is an
  `ItemList`, not a `Product`.
- Don't include fake reviews. Google catches this through known
  patterns (round ratings, sudden spikes, missing author names) and
  the penalty is severe.

For editorial pages (blog, guides), use the
[Article schema](/markup/schema-article.md) instead.
