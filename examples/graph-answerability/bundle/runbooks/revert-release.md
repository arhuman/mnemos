---
title: Reverting a Release
type: runbook
tags: [revert, canary, traffic]
---

# Reverting a Release

1. Identify the last known-good artifact from the registry.
2. Pin the service to that previous artifact version.
3. Shift traffic away from the current canary back to the stable fleet in 10%
   increments, watching error budgets between steps.
4. Freeze further changes until the incident channel confirms recovery.

The previous artifact is always retained for at least fourteen days, so this
recovery path is available even after several subsequent releases.
