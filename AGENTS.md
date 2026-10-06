# Working Rules

The source of truth is the current code, not a previous report. Read the README
and module contracts before modifying it. Preserve the license and applicable notices.

For each change, identify contract producers and consumers. Do not create implicit
migration aliases, unconfigured update paths, or valid example credentials.
Run the tests for the modified area and distinguish between unit tests, simulations,
native tests, and application builds.

Update catalogs, images, and documentation together with the code. Product mockups
are not runtime screenshots. Do not declare a target compiled if only the syntax
was verified, and do not promote a release without the requirements in `docs/RELEASE.md`.
