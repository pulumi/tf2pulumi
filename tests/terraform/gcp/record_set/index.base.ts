import * as pulumi from "@pulumi/pulumi";
import * as gcp from "@pulumi/gcp";

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
const prod = new gcp.dns.ManagedZone("prod", {
    dnsName: "prod.mydomain.com.",
});
const frontendRecordSet = new gcp.dns.RecordSet("frontend", {
    managedZone: prod.name,
    rrdatas: [frontendInstance.networkInterfaces.apply(networkInterfaces => networkInterfaces[0].accessConfigs![0].natIp!)],
    ttl: 300,
    type: "A",
});
