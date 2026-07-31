---
title: Multitenancy Overview
type: concept
tags: [multitenancy, tenant, noisy-neighbor]
---

# Multitenancy Overview

The platform is multi-tenant. A noisy neighbor tenant that consumes more than its
share can degrade others. The mechanics of keeping tenants apart are described in
the [tenant isolation](isolation.md) design.

Start there to understand how isolation is enforced before looking at any specific
mitigation.
