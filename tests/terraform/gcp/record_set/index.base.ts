import * as pulumi from "@pulumi/pulumi";
import * as gcp from "@pulumi/gcp";

// Originally defined at main.tf:11
const frontendInstance = new gcp.compute.Instance("frontend", {
    bootDisk: {
        initializeParams: {
            image: "debian-cloud/debian-9",
        },
    },
    machineType: "g1-small",
    networkInterfaces: [{
        accessConfigs: [{}],
        network: "default",
    }],
    zone: "us-central1-b",
});
// Originally defined at main.tf:28
const prod = new gcp.dns.ManagedZone("prod", {
    dnsName: "prod.mydomain.com.",
});
// Originally defined at main.tf:1
const frontendRecordSet = new gcp.dns.RecordSet("frontend", {
    managedZone: prod.name,
    rrdatas: [frontendInstance.networkInterfaces.apply(networkInterfaces => networkInterfaces[0].accessConfigs![0].natIp!)],
    ttl: 300,
    type: "A",
});
