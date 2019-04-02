package application

import (
	"context"
	"fmt"

	applicationv1alpha1 "github.com/sayaligaikawad/csye7374-operator/pkg/apis/application/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_application")

var updateApplication = false
var applicationStatusUpdated = false

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Application Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileApplication{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("application-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Application
	err = c.Watch(&source.Kind{Type: &applicationv1alpha1.Application{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Deployment and requeue the owner Application
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &applicationv1alpha1.Application{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileApplication{}

// ReconcileApplication reconciles a Application object
type ReconcileApplication struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Application object and makes changes based on the state read
// and what is in the Application.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Deployment as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileApplication) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Application")

	// Fetch the Application instance
	instance := &applicationv1alpha1.Application{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Define a new deployment object
	deployment := newDeploymentCR(instance)

	// Set Application instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, deployment, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Deployment already exists
	found := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.client.Create(context.TODO(), deployment)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Deployment created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	if int(*found.Spec.Replicas) != int(instance.Spec.Replicas) {
		reqLogger.Info(fmt.Sprintf("Replica miss match updating %v != %v", *found.Spec.Replicas, instance.Spec.Replicas))
		updateApplication = updateDeploymentReplicas(instance, found)
	}
	if found.Spec.Template.Spec.Containers[0].Image != instance.Spec.ApplicationVersion {
		reqLogger.Info("Application version miss match updating %v != %v", found.Spec.Template.Spec.Containers[0].Image, instance.Spec.ApplicationVersion)
		updateApplication = updateDeploymentAppVersion(instance, found)
	}

	if updateApplication == true {
		err = r.client.Update(context.TODO(), found)
		if err != nil {
			return reconcile.Result{}, err
		}

		objectKey, err := client.ObjectKeyFromObject(found)
		if err != nil {
			reqLogger.Error(err, "Unable to get name and namespace of Deployment object")
			return reconcile.Result{}, err
		}

		err = r.client.Get(context.TODO(), objectKey, found)
		if err != nil {
			reqLogger.Error(err, "Unable to update Deployment object")
			return reconcile.Result{}, err
		}

		applicationStatusUpdated = updateApplicationStatus(instance, found)

		if applicationStatusUpdated == true {
			err = r.client.Status().Update(context.TODO(), found)
			if err != nil {
				reqLogger.Error(err, "Unable to update CR status")
				return reconcile.Result{}, err
			}
		}
		// Deployment exists and was updated - don't requeue
		reqLogger.Info("Skip reconcile: Deployment exists and was updated", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
		return reconcile.Result{}, nil
	}

	// Deployment already exists and there are no changes - don't requeue
	reqLogger.Info("Skip reconcile: Deployment already exists but no changes", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
	return reconcile.Result{}, nil
}

// newDeploymentCR returns a busybox pod with the same name/namespace as the cr
func newDeploymentCR(cr *applicationv1alpha1.Application) *appsv1.Deployment {
	applicationVersion := cr.Spec.ApplicationVersion
	labels := map[string]string{
		"app": cr.Name,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-deployment",
			Namespace: cr.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(cr.Spec.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "web",
							Image: applicationVersion,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
}

func updateDeploymentReplicas(cr *applicationv1alpha1.Application, deployment *appsv1.Deployment) bool {
	deployment.Spec.Replicas = int32Ptr(cr.Spec.Replicas)
	return true
}

func updateDeploymentAppVersion(cr *applicationv1alpha1.Application, deployment *appsv1.Deployment) bool {
	deployment.Spec.Template.Spec.Containers[0].Image = cr.Spec.ApplicationVersion
	return true
}

func updateApplicationStatus(cr *applicationv1alpha1.Application, deployment *appsv1.Deployment) bool {
	cr.Status.Replicas = *deployment.Spec.Replicas
	cr.Status.ApplicationVersion = deployment.Spec.Template.Spec.Containers[0].Image
	return true
}

func int32Ptr(i int32) *int32 { return &i }
