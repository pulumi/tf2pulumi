# Programmatically Import Resources from an Existing Terraform State File

## Usage

See the [Adopting Pulumi From Terraform](https://www.pulumi.com/docs/guides/adopting/from_terraform/) guide
for an overview of how to adopt resources from your existing Terraform state files.

See [`main.go`](main.go) for how to do this in your Go Pulumi application.


## Missing Mappings

If you get a warning message during your import like the one below, add an additional 
mapping like `"aws_vpc": "aws:ec2/vpc:Vpc"` to `typeMapping` in 
[`resourceTypeMappings.go`](resourceTypeMappings.go) to map the Terraform resource to 
the Pulumi resource.

```
    warning: Resource mapping for [aws:s3/bucket:Bucket::main] not found. Skipping import...
```

## Special Mappings

If you get a warning message during your import like the one below, you likely need to add an 
additional _special_ mapping for resources that cannot be import by a simple `ID`. As an example, 
`aws_route_table_association` must be imported using the format `<subnetID>/<routeTableID>` so it 
requires a special function to form this mapping `ID`. Add this special mapping to `specialMapping` 
in [`resourceTypeMappings.go`](resourceTypeMappings.go).


```
    error: Preview failed: importing rtbassoc-02579bda627545f56: Unexpected format for import: rtbassoc-02579bda627545f56. Use 'subnet ID/route table ID' or 'gateway ID/route table ID
```
