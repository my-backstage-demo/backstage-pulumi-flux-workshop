import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";
import * as awsx from "@pulumi/awsx";

const cluster = new aws.ecs.Cluster("cluster", {});

const loadbalancer = new awsx.lb.ApplicationLoadBalancer("loadbalancer", {});

const service = new awsx.ecs.FargateService("service", {
    cluster: cluster.arn,
    assignPublicIp: true,
    taskDefinitionArgs: {
        container: {
            name: "awsx-ecs",
            image: "amazon/amazon-ecs-sample",
            cpu: 512,
            memory: 2048,
            essential: true,
            portMappings: [{
                containerPort: 80,
                targetGroup: loadbalancer.defaultTargetGroup,
            }],
        },
    },
});

// Export the URL so we can easily access it.
export const frontendURL = pulumi.interpolate`http://${loadbalancer.loadBalancer.dnsName}`;
