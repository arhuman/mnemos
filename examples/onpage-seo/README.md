# Example: On-Page SEO Essentials (real OKF bundle)

A realistic, 16-file [OKF](https://okfbundle.com) v0.1 knowledge bundle on
on-page SEO — title tags, meta descriptions, heading hierarchy, schema markup,
canonical URLs, Core Web Vitals, internal linking, and a content rubric. It is
vendored unmodified as example content (see [NOTICE.md](NOTICE.md) for source and
attribution) and exercises mnemos's OKF-native design (frontmatter, folders, and
§5.1 absolute cross-links) far more than a toy corpus.

## Try it

```bash
cd examples/onpage-seo/bundle   # the vendored bundle is the tree root
mnemos init
mnemos ingest . --collection seo

mnemos search "core web vitals" --collection seo      # -> technical/core-web-vitals.md
mnemos search "canonical urls" --collection seo       # -> technical/canonical-urls.md
mnemos search "heading hierarchy" --collection seo    # -> titles-and-meta/heading-hierarchy.md
mnemos ls --tree
```

The generated `.mnemos.toml` and `.mnemos/` are local state and are gitignored.

## Retrieval note

The default build uses lexical (FTS) search with AND semantics, so it matches
keyword/noun-phrase queries ("core web vitals", "canonical urls") well but misses
verbose natural-language phrasings whose terms don't all appear in the text. For
natural-language recall, build with semantic/hybrid search: `make install-embed`
(`-tags embed`).
