package deployer

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
)

var _ = Describe("Review evidence: NamespacedDeployer object tracking", func() {
	It("reports both applied objects with their namespace and live UID", func(ctx SpecContext) {
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		createServiceAccount(ctx, namespace.GetName(), true)
		resource := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: review-first
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: review-second
`)
		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resource.GetName(), nil)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		for _, name := range []string{"review-first", "review-second"} {
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.GetName(), Name: name}, cm)).To(Succeed())
			Expect(cm.GetUID()).NotTo(BeEmpty())
		}
		GinkgoWriter.Printf("Ready deployer status.deployed: %+v\n", deployer.Status.Deployed)
		Expect(deployer.Status.Deployed).To(HaveLen(2))
		for _, ref := range deployer.Status.Deployed {
			Expect(ref.Namespace).To(Equal(namespace.GetName()))
			Expect(ref.UID).NotTo(BeEmpty())
		}
	})

	It("finishes same-namespace deletion after its service account disappears", func(ctx SpecContext) {
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		createServiceAccount(ctx, namespace.GetName(), true)
		resource := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: review-cleanup
`)
		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resource.GetName(), nil)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.GetName(), Name: "review-cleanup"}, cm)).To(Succeed())
		Expect(metav1.GetControllerOf(cm).UID).To(Equal(deployer.GetUID()))
		GinkgoWriter.Printf("Before deletion status.deployed: %+v\n", deployer.Status.Deployed)

		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: namespace.GetName()}}
		Expect(k8sClient.Delete(ctx, sa)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			// Restore the account so the normal test cleanup can prune the ConfigMap.
			Expect(k8sClient.Create(ctx, sa)).To(Succeed())
		})
		Expect(k8sClient.Delete(ctx, deployer)).To(Succeed())
		Eventually(func(ctx context.Context) bool {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployer), deployer)
			if err == nil {
				GinkgoWriter.Printf("Deletion timestamp: %v; finalizers: %v\n", deployer.GetDeletionTimestamp(), deployer.GetFinalizers())
			}
			return apierrors.IsNotFound(err)
		}).WithTimeout(5 * time.Second).WithContext(ctx).Should(BeTrue())
	})
})
