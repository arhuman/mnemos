---
type: Reference
title: Heading hierarchy
description: One H1, properly nested H2s and H3s, semantic outlines that match the content. The rules are short.
resource: https://www.w3.org/WAI/tutorials/page-structure/headings/
tags: [headings, h1, semantic-html, accessibility]
timestamp: 2026-06-19T08:00:00Z
---

# Heading hierarchy

Search engines parse heading levels as a content outline. Screen
readers do the same for users navigating the page. The two purposes
overlap completely; following accessibility rules also satisfies SEO.

## The four rules

1. **Exactly one `<h1>`** per page. Match it to the page intent.
2. **No level skipping**. H1 → H2 → H3 is fine; H1 → H3 is broken.
3. **Headings describe their section.** A heading promises content
   below it; that content has to deliver.
4. **No styled `<div>`s in place of headings.** If it looks like a
   heading, mark it up as one.

## Title vs H1

The [title tag](/titles-and-meta/title-tag.md) and the H1 can differ.
Title is for the SERP, H1 is for the page. The title is often more
keyword-loaded; the H1 is often more reader-loaded.

## In tools

Most CMSs make this hard. The blog post editor lets you pick "Heading
1" from a dropdown, but that's the H1 inside the body — and the
template already has an H1 (the article title). Result: two H1s. Audit
this on every new template before launch.

If you're maintaining a bundle of markdown like this one, the rules
above apply: one `# heading` per file, no `# heading` mid-document.
The site renderer wraps it in your template's H1, so use H2/H3 for
section structure.
