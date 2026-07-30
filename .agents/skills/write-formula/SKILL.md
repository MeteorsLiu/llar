---
name: write-formula
description: Create, migrate, review, debug, and validate LLAR formulas, including versions.json, _llar.gox build classfiles, _cmp.gox comparators, direct dependencies, build matrices, CMake or Autotools recipes, metadata, and onTest consumer checks. Use when adding a library to LLAR, changing an existing formula, porting old Formula hooks, or fixing formula parsing, dependency, build, matrix, cache, or test failures.
---

# Write LLAR Formulas

## Read First

Read both references completely before editing a `.gox` file:

1. [XGo Classfile skill](references/xgo-classfile/SKILL.md)
2. [LLAR Formula guide](references/llar-formula.md)

## Workflow

1. Confirm the module id and exact source version. Preserve the tag spelling.
2. Inspect that version's build files, dependency manifests or lock files,
   install rules, package-config files, and tests.
3. Record the supported build entrypoint, enabled configuration, direct
   dependencies, platform limitations, installed outputs, and consumer usage.
4. Inspect the module's existing `versions.json`, formula thresholds, and
   optional comparator.
5. Update only the required `versions.json`, `_llar.gox`, and optional
   `_cmp.gox` files.
6. Add `onTest` when an installed-output consumer check is available.
7. Run `llar test` for the exact version and every affected matrix selection.

## Rules

- Keep `versions.json.path`, formula `id`, dependency module ids, and version
  spelling consistent.
- Declare direct dependencies only.
- Use `onBuild ctx` and `onTest ctx`.
- Use `target.require` for propagated environment dimensions.
- Use `target.options` for package-owned choices.
- Keep options independent; use `filter` only for unsupported selections.
- Treat `defaults` as option defaults, not a legal-value schema.
- Call CMake and Autotools configure/build/install methods directly.
- Check `lastErr` after `exec` when command failure must fail the hook.
- Derive metadata from installed output and consumer behavior.
- Make `onTest` work after both a fresh build and a cache hit.
- Add a comparator only when actual version tags require one.
- Derive every flag and dependency from the selected source version.
- Do not add unverified compatibility flags, fallbacks, or optional features.

Do not use:

- `onBuild (ctx, proj, out)` or `onTest (ctx, proj, out)`;
- `out.addErr`, `BuildResult.AddErr`, or `TestResult`;
- `ctx.currentMatrix()` or a top-level `matrix` declaration;
- generated `XGot_`, `Gopt_`, `Gops_`, `Gopx_`, `Gopo_`, `__0`, or `__1`
  names in formula source;
- `err := c.configure()` or equivalent checks for current CMake or Autotools
  helpers.

## Validation

```sh
llar test --verbose ./owner/repo@exact-version \
  --os "$(go env GOOS)" --arch "$(go env GOARCH)"
```

Add each affected `--option key=value` and required dimension. Verify:

1. The intended formula is selected.
2. Dependencies and versions match the selected source.
3. Expected headers, libraries, tools, and package-config files are installed.
4. Metadata works in a consumer operation.
5. Defaults and supported options build.
6. Unsupported selections are rejected by `filter`.
7. `onTest` passes after a fresh build and a cache hit.
