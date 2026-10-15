package deployer

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
)

// envtest runs no garbage collector. An owner reference to the deleted deployer that survives the deletion means the
// garbage collector of a real cluster deletes the object, which the Orphan deletion policy promises to keep.
var _ = Describe("Review evidence: Orphan deletion policy without service account", func() {
	It("keeps same-namespace objects when the service account is gone", func(ctx SpecContext) {
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		createServiceAccount(ctx, namespace.GetName(), true)
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), `apiVersion: v1
kind: ConfigMap
metadata:
  name: review-orphan-sa-gone-cm
`)

		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil, func(d *v1alpha1.NamespacedDeployer) {
			d.Spec.DeletionPolicy = v1alpha1.DeletionPolicyOrphan
		})
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})
		deployerUID := deployer.GetUID()

		By("removing the service account like `kubectl delete -f bootstrap.yaml` does, then the deployer")
		test.DeleteObject(ctx, k8sClient, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: namespace.GetName()}})
		test.DeleteObject(ctx, k8sClient, deployer)

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.GetName(), Name: "review-orphan-sa-gone-cm"}, cm)).To(Succeed())
		GinkgoWriter.Printf("deleted deployer UID=%s, ConfigMap owners=%+v\n", deployerUID, cm.GetOwnerReferences())
		Expect(cm.GetOwnerReferences()).NotTo(ContainElement(HaveField("UID", deployerUID)),
			"Orphan policy: the garbage collector deletes the ConfigMap through this owner reference")
	})
	It("blames the service account when it may not prune", func(ctx SpecContext) {
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		createServiceAccount(ctx, namespace.GetName(), true)
		other := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace.GetName() + "-other"}}
		Expect(k8sClient.Create(ctx, other)).To(Succeed())
		grantConfigMaps(ctx, other.GetName(), namespace.GetName(), namespacedDeployerSA)
		resourceObj := mockYAMLResource(ctx, namespace.GetName(), fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: review-msg-cm
  namespace: %s
`, other.GetName()))
		deployer := createNamespacedDeployer(ctx, namespace.GetName(), resourceObj.GetName(), nil)
		test.WaitForReadyObject(ctx, k8sClient, deployer, map[string]any{})

		By("revoking the service account's permissions in the other namespace; the controller can still read the service account")
		binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: namespacedDeployerSA, Namespace: other.GetName()}}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(binding), binding)).To(Succeed())
		test.DeleteObject(ctx, k8sClient, binding.DeepCopy())
		Expect(k8sClient.Delete(ctx, deployer)).To(Succeed())
		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, deployer, v1alpha1.DeletionFailedReason, "review-msg-cm")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.GetName(), Name: namespacedDeployerSA}, &corev1.ServiceAccount{})).To(Succeed())

		cond := apimeta.FindStatusCondition(deployer.Status.Conditions, v1alpha1.ReadyCondition)
		Expect(cond).NotTo(BeNil())
		msg := cond.Message
		GinkgoWriter.Printf("DeletionFailed message: %s\n", msg)
		By("restoring the permissions so the deletion finishes")
		binding.ResourceVersion = ""
		binding.UID = ""
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		Expect(msg).NotTo(ContainSubstring("the controller cannot read service account"),
			"the forbidden request came from the impersonated service account, not from the controller reading it")

	})
})
