package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecs"
	ecs2 "github.com/pulumi/pulumi-awsx/sdk/go/awsx/ecs"
	"github.com/pulumi/pulumi-awsx/sdk/go/awsx/lb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create an AWS resource (S3 Bucket)
		ecs, err := ecs.NewCluster(ctx, "ecs", nil)
		if err != nil {
			return err
		}

		applicationLoadBalancer, err := lb.NewApplicationLoadBalancer(ctx, "web", nil)
		if err != nil {
			return err
		}

		ecs2.NewFargateService(ctx, "web", &ecs2.FargateServiceArgs{
			Cluster:        ecs.Arn,
			AssignPublicIp: pulumi.Bool(true),
			TaskDefinitionArgs: &ecs2.FargateServiceTaskDefinitionArgs{
				Container: &ecs2.TaskDefinitionContainerDefinitionArgs{
					Name:      pulumi.String("awsx-ecs"),
					Image:     pulumi.String("amazon/amazon-ecs-sample"),
					Cpu:       pulumi.Int(512),
					Memory:    pulumi.Int(2048),
					Essential: pulumi.Bool(true),
					PortMappings: &ecs2.TaskDefinitionPortMappingArray{
						&ecs2.TaskDefinitionPortMappingArgs{
							ContainerPort: pulumi.Int(80),
							TargetGroup:   applicationLoadBalancer.DefaultTargetGroup,
						},
					},
				},
			},
		})

		ctx.Export("frontendURL", pulumi.Sprintf("http://%s", applicationLoadBalancer.LoadBalancer.DnsName()))
		return nil
	})
}
