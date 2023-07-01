/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"net"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingk8siov1 "ahmed.com/networkpolicy/api/v1"
	knetworking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NetworkWhitelistReconciler reconciles a NetworkWhitelist object
type NetworkWhitelistReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.k8s.io.ahmed.com,resources=networkwhitelists,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io.ahmed.com,resources=networkwhitelists/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.k8s.io.ahmed.com,resources=networkwhitelists/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NetworkWhitelist object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *NetworkWhitelistReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var networkWhitelist networkingk8siov1.NetworkWhitelist
	if err := r.Get(ctx, req.NamespacedName, &networkWhitelist); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	name := fmt.Sprintf("%s-generated-networkpolicy", networkWhitelist.Name)
	var networkPolicyIngressRuleList []knetworking.NetworkPolicyIngressRule
	for _, domain := range networkWhitelist.Spec.Domains {
		ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip4", domain)
		if err != nil {
			log.Log.Error(err, fmt.Sprintf("couldn't resolve domain %s", domain))
		}
		for _, ip := range ips {
			ipBlock := knetworking.IPBlock{
				CIDR: fmt.Sprintf("%s/32", ip.String()),
			}
			networkPolicyIngressRule := knetworking.NetworkPolicyIngressRule{
				From: []knetworking.NetworkPolicyPeer{
					{
						IPBlock: &ipBlock,
					},
				},
			}
			networkPolicyIngressRuleList = append(networkPolicyIngressRuleList, networkPolicyIngressRule)
		}
	}

	networkPolicy := knetworking.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   networkWhitelist.Namespace,
		},
		Spec: knetworking.NetworkPolicySpec{
			Ingress: networkPolicyIngressRuleList,
		},
		Status: knetworking.NetworkPolicyStatus{},
	}

	if err := ctrl.SetControllerReference(&networkWhitelist, &networkPolicy, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, &networkPolicy); err != nil {
		log.Log.Error(err, "unable to create Job for CronJob", "networkpolicy", &networkPolicy)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

var (
	npOwnerKey = ".metadata.controller"
	apiGVStr   = networkingk8siov1.GroupVersion.String()
)

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkWhitelistReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &knetworking.NetworkPolicy{}, npOwnerKey, func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		np := rawObj.(*knetworking.NetworkPolicy)
		owner := metav1.GetControllerOf(np)
		if owner == nil {
			return nil
		}
		// ...make sure it's a CronJob...
		if owner.APIVersion != apiGVStr || owner.Kind != "NetworkWhitelist" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingk8siov1.NetworkWhitelist{}).
		Owns(&knetworking.NetworkPolicy{}).
		Complete(r)
}
