---
title: Secrets Storage
type: concept
tags: [secrets, security]
---

# Secrets Storage

Application secrets are stored in the managed secrets vault, never in the repo or
in environment files committed to git. Services read secrets at boot through a
short-lived token issued by the vault agent.

Rotate a stored secret by writing a new version in the vault; running services
pick it up on their next lease renewal.
