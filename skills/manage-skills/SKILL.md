---
name: manage-skills
description: Detect available skill installers, summarize user intent into relevant skill namespaces, query matching skills, and install the selected skills. Use when Codex needs to check whether the current host has a builtin skill installer, find installable skills for a user request, or install a skill by namespace or verified source path.
---

# Manage Skills

Detect a usable skill installer, turn the user's request into concrete skill namespaces, and install the selected skills through the detected installer.

Prefer verified evidence over host-name assumptions. If no installer can be verified, say so directly and ask for the installer location or supported source instead of guessing.

Use `github.com/MeteorsLiu/skills-registry` as the default hosted registry.

## Workflow

1. Read the request and classify it:
- installer detection only
- skill discovery only
- discovery plus installation
- installation from a known namespace or source path

2. Detect a skill installer before searching:
- Prefer a user-provided installer path, command, or source.
- Prefer concrete filesystem or command evidence over product-name inference.
- For Codex, verify `$CODEX_HOME/skills/.system/skill-installer/` or `~/.codex/skills/.system/skill-installer/`.
- Confirm `SKILL.md`, `scripts/list-skills.py`, and `scripts/install-skill-from-github.py` exist before treating Codex as supported.
- Do not claim another host has no installer unless there is concrete evidence. Treat missing evidence as unverified, not absent.

3. Summarize the user's goal into a search brief:
- extract the actual task the user wants help with
- remove unrelated wording
- keep domain, tooling, and workflow constraints
- convert that brief into candidate skill keywords or namespaces

4. Query relevant skills:
- Use `github.com/MeteorsLiu/skills-registry/INDEX.md` as the primary hosted search surface.
- Read `INDEX.md` first and use its names, summaries, and tags to narrow candidates.
- Prefer the installer's native listing only when it is the authoritative search surface for that host.
- Collect candidate skill namespaces that actually match the user's goal.
- Never invent a namespace, repo path, or source URL.

5. Resolve the namespace:
- If one candidate clearly matches, state it and continue.
- If multiple candidates remain, present the short list with one-line tradeoffs and recommend one.
- If no candidate matches, stop and say that no verified skill was found.

6. Resolve the version:
- If the user requested an exact version and that version exists, use it.
- Use the hosted registry's published versions as the source of truth for available versions.
- If `{owner}/{repo}/COMPARATOR.md` exists, read it and use it only to compare candidate versions.
- If no repo-level comparator guide exists, fall back to GNU-style version comparison.
- If the user did not request a version, enumerate available versions and choose the maximum version by the comparator.
- If multiple requirements mention different versions of the same skill repo, keep the maximum requested version by the same comparator.
- Do not apply implicit `<= target` fallback unless the local workflow explicitly supports it for that repo.

7. Install through the detected installer:
- Use the verified namespace or source path.
- Preserve host-specific follow-up steps.
- For Codex, remind the user to restart Codex after installation.
- If installation fails, report the exact failure and the verified source that was attempted.

## Decision Rules

- Detection only: stop after reporting installer status and any verified capabilities.
- Discovery only: return the candidate namespaces and the recommended choice.
- Discovery plus install: continue through installation after a single clear match or user confirmation.
- No verified installer: explain what evidence is missing and ask for a supported installer path, command, or source.
- No verified skill match: say so directly; do not install a near match without approval.

## Output

Return these fields in a compact summary:

- detected installer status
- evidence used for detection
- verified source used for discovery
- candidate namespaces
- selected namespace
- selected version
- version comparison evidence
- installation result
- required follow-up steps

## Example Requests

- `Check whether this environment has a skill installer and install the right skill for Terraform modules.`
- `Find a skill for writing PR descriptions, show the namespace, then install it.`
- `I want a PDF skill. Detect the installer, search relevant namespaces, and install the best match.`
