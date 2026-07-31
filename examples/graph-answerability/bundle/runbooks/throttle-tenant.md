---
title: Throttle Runbook
type: runbook
tags: [ratelimit, quota]
---

# Throttle Runbook

1. Identify the offending workload from the per-quota dashboard.
2. Apply a temporary rate limit at the ingress for that workload's key.
3. Notify the workload owner with the observed consumption numbers.
4. Lift the limit once consumption returns under the agreed quota.

Rate limits applied here are temporary; a persistent offender is escalated to a
capacity review instead.
