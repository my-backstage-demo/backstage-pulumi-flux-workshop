package gitops

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/apiextensions"
	v12 "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/apps/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Flux struct {
	pulumi.ResourceState

	Namespace pulumi.StringOutput `pulumi:"namespace"`
}

type FluxBootstrapArgs struct {
	RepoURL pulumi.StringInput
	Branch  pulumi.StringInput
	Path    pulumi.StringInput
}

type FluxArgs struct {
	Version     pulumi.StringInput
	ClusterName pulumi.StringInput
	Bootstrap   FluxBootstrapArgs
}

func NewFlux(ctx *pulumi.Context, name string, args *FluxArgs, opts ...pulumi.ResourceOption) (*Flux, error) {
	var flux Flux
	err := ctx.RegisterComponentResource("pkg:index:Flux", name, &flux, opts...)
	if err != nil {
		return nil, err
	}

	backStageLabel := pulumi.StringMap{
		"backstage.io/kubernetes-id": args.ClusterName,
	}

	fluxHelm, err := helm.NewRelease(ctx, "pulumi-flux2", &helm.ReleaseArgs{
		Chart:           pulumi.String("oci://ghcr.io/fluxcd-community/charts/flux2"),
		Namespace:       pulumi.String("flux-system"),
		CreateNamespace: pulumi.Bool(true),
		Version:         args.Version,
		Values: pulumi.Map{
			"helmController": pulumi.Map{
				"labels": backStageLabel,
			},
			"kustomizeController": pulumi.Map{
				"labels": backStageLabel,
			},
			"notificationController": pulumi.Map{
				"labels": backStageLabel,
			},
			"sourceController": pulumi.Map{
				"labels": backStageLabel,
			},
			"imageReflectionController": pulumi.Map{
				"labels": backStageLabel,
			},
			"imageAutomationController": pulumi.Map{
				"labels": backStageLabel,
			},
		},
	}, pulumi.Parent(&flux))
	if err != nil {
		return nil, err
	}

	for _, controller := range []string{"kustomize-controller", "helm-controller", "notification-controller", "source-controller", "image-reflector-controller", "image-automation-controller"} {
		_, err = v12.NewDeploymentPatch(ctx, fmt.Sprintf("pulumi-%s-patch", controller), &v12.DeploymentPatchArgs{
			Metadata: &metav1.ObjectMetaPatchArgs{
				Annotations: pulumi.StringMap{
					"pulumi.com/patchForce": pulumi.String("true"),
				},
				Name:      pulumi.String(controller),
				Namespace: fluxHelm.Namespace,
				Labels:    backStageLabel,
			},
		}, pulumi.Parent(&flux), pulumi.DependsOn([]pulumi.Resource{fluxHelm}))
		if err != nil {
			return nil, err
		}
	}

	boostrapRepo, err := apiextensions.NewCustomResource(ctx, "pulumi-bootstrap-repo", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("source.toolkit.fluxcd.io/v1"),
		Kind:       pulumi.String("GitRepository"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("bootstrap-repo"),
			Labels: pulumi.StringMap{
				"backstage.io/kubernetes-id": pulumi.String("gitops-cluster"),
			},
			Namespace: fluxHelm.Namespace,
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": pulumi.Map{
				"interval": pulumi.String("1m"),
				"ref": pulumi.Map{
					"branch": args.Bootstrap.Branch,
				},
				"timeout": pulumi.String("60s"),
				"url":     args.Bootstrap.RepoURL,
			},
		},
	}, pulumi.Parent(&flux), pulumi.DependsOn([]pulumi.Resource{fluxHelm}))
	if err != nil {
		return nil, err
	}
	_, err = apiextensions.NewCustomResource(ctx, "pulumi-bootstrap-kustomization", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("kustomize.toolkit.fluxcd.io/v1"),
		Kind:       pulumi.String("Kustomization"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("bootstrap-kustomization"),
			Labels: pulumi.StringMap{
				"backstage.io/kubernetes-id": pulumi.String("gitops-cluster"),
			},
			Namespace: boostrapRepo.Metadata.Namespace(),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": pulumi.Map{
				"force":    pulumi.Bool(false),
				"interval": pulumi.String("1m"),
				"prune":    pulumi.Bool(true),
				"path":     args.Bootstrap.Path,
				"sourceRef": pulumi.Map{
					"kind":      boostrapRepo.Kind,
					"name":      boostrapRepo.Metadata.Name(),
					"namespace": boostrapRepo.Metadata.Namespace(),
				},
				"targetNamespace": boostrapRepo.Metadata.Namespace(),
			},
		},
	}, pulumi.Parent(&flux))
	if err != nil {
		return nil, err
	}

	flux.Namespace = fluxHelm.Namespace.Elem()
	if err := ctx.RegisterResourceOutputs(&flux, pulumi.Map{
		"namespace": flux.Namespace,
	}); err != nil {
		return nil, err
	}

	return &flux, nil
}
