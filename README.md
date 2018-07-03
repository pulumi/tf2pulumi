# HCL -> Pulumi converter

## TODO
- comments/docs
- data sources:
	- explicit dependencies
- modules
	- generate child module as a function
	- call function to instantiate
- variables accesses:
	- path
	- self
	- simple
	- terraform
- calls:
	- various TF functions
- types:
	- runtime conversions from unknowns to accurate types
	- runtime list flattening (necessary if any element of a list has an unknown type)
