## v0.4.7 (Unreleased)

## v0.4.6 (Released February 1, 2019)

### Improvements

- Added support for the Pulumi f5bigip provider
- Simplified string literal generation for literals that contain a single leading or trailing newline

## v0.4.5 (Released February 1, 2019)

### Improvements

- Improved the CLI experience. Note that this requires that `-allow-missing-{plugins,variables}` are now preceded by
  two dashes rather than one.
- Fixed a bug where tf2pulumi would generate object keys that contained invalid characters
- Improved string literal generation to emit template literals for strings that contain newlines
- Improved template literal generation to avoid emitting interpolations that are string literals (e.g. `${"."}`)

## v0.4.4 (Released January 31, 2019)

### Improvements

- Added the ability to continue code generation in the face of references to missing resources, data sources, etc. by
  passing the `-allow-missing-variables` flag

## v0.4.3 (Released January 11, 2019)

### Improvements

- Fixed a bug where unknown resources would cause `tf2pulumi` to crash
- Fixed a bug where circular local references would cause `tf2pulumi` to crash
- Fixed a bug where forward references between locals would cause `tf2pulumi` to crash
