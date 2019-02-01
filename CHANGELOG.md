## v0.4.4 (Released January 31, 2019)

### Improvements

- Added the ability to continue code generation in the face of references to missing resources, data sources, etc. by
  passing the `-allow-missing-variables` flag

## v0.4.3 (Released January 11, 2019)

### Improvements

- Fixed a bug where unknown resources would cause `tf2pulumi` to crash
- Fixed a bug where circular local references would cause `tf2pulumi` to crash
- Fixed a bug where forward references between locals would cause `tf2pulumi` to crash
