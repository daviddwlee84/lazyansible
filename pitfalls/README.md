# Pitfalls

Search this directory by the symptom. Add a dedicated note only for a resolved,
non-obvious or recurring trap, preserving exact error text and the tested fix.
Future investigations belong in [TODO.md](../TODO.md) and [backlog/](../backlog/README.md).
Serious repeated rules belong in [AGENTS.md](../AGENTS.md), with a link back to the evidence.

## Resolved incidents

| Note | Symptom | Status |
| --- | --- | --- |
| [Ansible listing exceeds output cap](ansible-listing-exceeds-output-cap.md) | `oversized observation accepted: 4194305 <nil>` | Fixed; subprocess regression retained |

## Cross-referenced workflow caveats

These current integration caveats are already documented with their normal workflow:

| Symptom | Documentation | Cause / boundary |
| --- | --- | --- |
| Host counters miss custom callback output | [Inspect and automate](../README.md#inspect-and-automate) | Managed child runs use the default callback; global config stays unchanged |
| An old run cannot be replayed | [Preferences and storage](../README.md#preferences-and-storage) | Original cwd or sensitive inputs were not retained; configure again |
| Ansible update is unknown or constrained | [Shared Ansible runtime](../README.md#shared-ansible-runtime) | Unknown owner/index policy/network state or installed uv constraints |

These are maintainer notes, excluded from binary archive contents. They are not
an automatic secret-redaction boundary; review content before publishing.
