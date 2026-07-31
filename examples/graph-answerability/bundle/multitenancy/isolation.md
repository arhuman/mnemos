---
title: Tenant Isolation
type: concept
tags: [isolation, quota]
---

# Tenant Isolation

Isolation is enforced with per-tenant resource quotas and separate connection
pools. When one tenant exceeds its quota, the concrete mitigation, applying a
rate limit, is carried out per the [throttle runbook](../runbooks/throttle-tenant.md).

This page covers the design; the runbook covers the hands-on remediation.
