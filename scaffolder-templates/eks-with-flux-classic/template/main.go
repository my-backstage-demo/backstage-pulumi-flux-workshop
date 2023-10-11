package main

import (
	"fmt"
	"math/rand"
	"os"
	"template/internal/gitops"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/iam"
	"github.com/pulumi/pulumi-awsx/sdk/go/awsx/ec2"
	"github.com/pulumi/pulumi-eks/sdk/go/eks"
	"github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

const (
	albNamespace      = "aws-lb-controller"
	albServiceAccount = "system:serviceaccount:" + albNamespace + ":aws-lb-controller-serviceaccount"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		awsConfig := config.New(ctx, "aws")
		// Get some configuration values or set default values
		cfg := config.New(ctx, "")
		minClusterSize, err := cfg.TryInt("minClusterSize")
		if err != nil {
			minClusterSize = 3
		}
		maxClusterSize, err := cfg.TryInt("maxClusterSize")
		if err != nil {
			maxClusterSize = 6
		}
		desiredClusterSize, err := cfg.TryInt("desiredClusterSize")
		if err != nil {
			desiredClusterSize = 3
		}
		eksNodeInstanceType, err := cfg.Try("eksNodeInstanceType")
		if err != nil {
			eksNodeInstanceType = "t3.medium"
		}
		vpcNetworkCidr, err := cfg.Try("vpcNetworkCidr")
		if err != nil {
			vpcNetworkCidr = "10.0.0.0/16"
		}
		clusterName, err := cfg.Try("clusterName")
		if err != nil {
			rand.New(rand.NewSource(time.Now().UnixNano()))
			clusterName = petname.Generate(3, "-")
		}

		// Create a new VPC, subnets, and associated infrastructure
		eksVpc, err := ec2.NewVpc(ctx, "eks-vpc", &ec2.VpcArgs{
			EnableDnsHostnames: pulumi.Bool(true),
			CidrBlock:          &vpcNetworkCidr,
		})
		if err != nil {
			return err
		}

		// Create a new EKS cluster
		eksCluster, err := eks.NewCluster(ctx, "eks-cluster", &eks.ClusterArgs{
			// Put the cluster in the new VPC created earlier
			VpcId: eksVpc.VpcId,
			// Public subnets will be used for load balancers
			PublicSubnetIds: eksVpc.PublicSubnetIds,
			// Private subnets will be used for cluster nodes
			PrivateSubnetIds: eksVpc.PrivateSubnetIds,
			// Change configuration values above to change any of the following settings
			InstanceType:    pulumi.String(eksNodeInstanceType),
			DesiredCapacity: pulumi.Int(desiredClusterSize),
			MinSize:         pulumi.Int(minClusterSize),
			MaxSize:         pulumi.Int(maxClusterSize),
			// Do not give the worker nodes a public IP address
			NodeAssociatePublicIpAddress: pulumi.BoolRef(false),
			Name:                         pulumi.String(clusterName),
			CreateOidcProvider:           pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		// Export some values in case they are needed elsewhere
		ctx.Export("kubeconfig", pulumi.ToSecret(eksCluster.Kubeconfig))

		// enable ALB
		albRole, err := iam.NewRole(ctx, "alb-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.All(eksCluster.Core.OidcProvider().Arn(), eksCluster.Core.OidcProvider().Url()).ApplyT(func(args []interface{}) string {
				arn := args[0].(string)
				url := args[1].(string)
				assumeRolePolicy, _ := iam.GetPolicyDocument(ctx, &iam.GetPolicyDocumentArgs{
					Statements: []iam.GetPolicyDocumentStatement{
						{
							Effect: pulumi.StringRef("Allow"),
							Actions: []string{
								"sts:AssumeRoleWithWebIdentity",
							},
							Principals: []iam.GetPolicyDocumentStatementPrincipal{
								{
									Type: "Federated",
									Identifiers: []string{
										arn,
									},
								},
							},
							Conditions: []iam.GetPolicyDocumentStatementCondition{
								{
									Test: "StringEquals",
									Values: []string{
										albServiceAccount,
									},
									Variable: fmt.Sprintf("%s:sub", url),
								},
							},
						},
					},
				})
				return assumeRolePolicy.Json
			}).(pulumi.StringOutput),
		})
		if err != nil {
			return err
		}

		albPolicyFile, err := os.ReadFile("./iam-policies/alb-iam-policy.json")
		if err != nil {
			return err
		}

		albIAMPolicy, err := iam.NewPolicy(ctx, "alb-policy", &iam.PolicyArgs{
			Policy: pulumi.String(albPolicyFile),
		}, pulumi.DependsOn([]pulumi.Resource{albRole}))
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicyAttachment(ctx, "alb-role-attachment", &iam.RolePolicyAttachmentArgs{
			PolicyArn: albIAMPolicy.Arn,
			Role:      albRole.Name,
		}, pulumi.DependsOn([]pulumi.Resource{albIAMPolicy}))
		if err != nil {
			return err
		}

		k8sProvider, err := kubernetes.NewProvider(ctx, "kubernetes-provider", &kubernetes.ProviderArgs{
			Kubeconfig:            eksCluster.KubeconfigJson,
			EnableServerSideApply: pulumi.Bool(true),
		}, pulumi.DependsOn([]pulumi.Resource{eksCluster}))
		if err != nil {
			return err
		}

		flux, err := gitops.NewFlux(ctx, "flux", &gitops.FluxArgs{
			Version:     pulumi.String("2.10.1"),
			ClusterName: eksCluster.EksCluster.Name(),
			Bootstrap: gitops.FluxBootstrapArgs{
				RepoURL: pulumi.String("https://github.com/my-backstage-demo/pulumi-gitops-repo.git"),
				Branch:  pulumi.String("main"),
				Path:    pulumi.String("./flux/clusters/aws"),
			},
		}, pulumi.ProviderMap(map[string]pulumi.ProviderResource{
			"kubernetes": k8sProvider,
		}))
		if err != nil {
			return err
		}
		_, err = v1.NewSecret(ctx, "aws-lb-controller-secret", &v1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("aws-load-balancer-controller-values"),
				Namespace: flux.Namespace,
			},
			StringData: pulumi.StringMap{
				"values.yaml": pulumi.Sprintf(`clusterName: %s
region: %s
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: %s
vpcId: %s`, eksCluster.EksCluster.Name(), awsConfig.Get("region"), albRole.Arn, eksVpc.VpcId),
			},
		}, pulumi.Provider(k8sProvider))
		if err != nil {
			return err
		}

		return nil
	})
}
