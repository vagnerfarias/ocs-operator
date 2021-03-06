package storagecluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	openshiftv1 "github.com/openshift/api/template/v1"
	v1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	coreEnvVar          = "NOOBAA_CORE_IMAGE"
	dbEnvVar            = "NOOBAA_DB_IMAGE"
	defaultStorageClass = "noobaa-ceph-rbd"
)

var noobaaReconcileTestLogger = logf.Log.WithName("noobaa_system_reconciler_test")

func TestEnsureNooBaaSystem(t *testing.T) {
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: "test_ns",
	}
	sc := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
	}
	noobaa := v1alpha1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
		},
	}

	cephCluster := cephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephClusterFromString(namespacedName.Name),
			Namespace: namespacedName.Namespace,
		},
	}
	cephCluster.Status.State = cephv1.ClusterStateCreated

	addressableStorageClass := defaultStorageClass

	cases := []struct {
		label          string
		namespacedName types.NamespacedName
		sc             v1.StorageCluster
		noobaa         v1alpha1.NooBaa
		isCreate       bool
	}{
		{
			label:          "case 1", //ensure create logic
			namespacedName: namespacedName,
			sc:             sc,
			noobaa:         noobaa,
			isCreate:       true,
		},
		{
			label:          "case 2", //ensure update logic
			namespacedName: namespacedName,
			sc:             sc,
			noobaa:         noobaa,
		},
		{
			label:          "case 3", //equal, no update
			namespacedName: namespacedName,
			sc:             sc,
			noobaa: v1alpha1.NooBaa{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
					SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
				},
				Spec: v1alpha1.NooBaaSpec{
					DBStorageClass:            &addressableStorageClass,
					PVPoolDefaultStorageClass: &addressableStorageClass,
				},
			},
		},
	}

	for _, c := range cases {
		reconciler := getReconciler(t, &v1alpha1.NooBaa{})
		reconciler.client.Create(context.TODO(), &cephCluster)

		if c.isCreate {
			err := reconciler.client.Get(context.TODO(), namespacedName, &c.noobaa)
			assert.True(t, errors.IsNotFound(err))
		} else {
			err := reconciler.client.Create(context.TODO(), &c.noobaa)
			assert.NoError(t, err)
		}
		err := reconciler.ensureNoobaaSystem(&sc, noobaaReconcileTestLogger)
		assert.NoError(t, err)

		noobaa = v1alpha1.NooBaa{}
		err = reconciler.client.Get(context.TODO(), namespacedName, &noobaa)
		assert.Equal(t, noobaa.Name, namespacedName.Name)
		assert.Equal(t, noobaa.Namespace, namespacedName.Namespace)
		if !c.isCreate {
			assert.Equal(t, *noobaa.Spec.DBStorageClass, defaultStorageClass)
			assert.Equal(t, *noobaa.Spec.PVPoolDefaultStorageClass, defaultStorageClass)
		}
	}
}

func TestSetNooBaaDesiredState(t *testing.T) {
	defaultInput := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test_name",
		},
	}
	cases := []struct {
		label   string
		envCore string
		envDB   string
		sc      v1.StorageCluster
	}{
		{
			label:   "case 1", // both envVars carry through to created NooBaaSystem
			envCore: "FOO",
			envDB:   "BAR",
			sc:      defaultInput,
		},
		{
			label: "case 2", // missing core envVar causes no issue
			envDB: "BAR",
			sc:    defaultInput,
		},
		{
			label:   "case 3", // missing db envVar causes no issue
			envCore: "FOO",
			sc:      defaultInput,
		},
		{
			label: "case 4", // neither envVar set, no issues occur
			sc:    defaultInput,
		},
		{
			label: "case 5", // missing initData namespace does not cause error
			sc:    v1.StorageCluster{},
		},
	}

	for _, c := range cases {

		err := os.Setenv(coreEnvVar, c.envCore)
		if err != nil {
			assert.Failf(t, "[%s] unable to set env_var %s", c.label, coreEnvVar)
		}
		err = os.Setenv(dbEnvVar, c.envDB)
		if err != nil {
			assert.Failf(t, "[%s] unable to set env_var %s", c.label, dbEnvVar)
		}

		reconciler := ReconcileStorageCluster{}
		reconciler.initializeImageVars()

		noobaa := v1alpha1.NooBaa{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NooBaa",
				APIVersion: "noobaa.io/v1alpha1'",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "noobaa",
				Namespace: defaultInput.Namespace,
			},
		}
		err = reconciler.setNooBaaDesiredState(&noobaa, &c.sc)
		if err != nil {
			assert.Failf(t, "[%s] unable to set noobaa desired state", c.label)
		}

		assert.Equalf(t, noobaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.NotEmptyf(t, noobaa.Labels, "[%s] expected noobaa Labels not found", c.label)
		assert.Equalf(t, noobaa.Labels["app"], "noobaa", "[%s] expected noobaa Label mismatch", c.label)
		assert.Equalf(t, noobaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.Equal(t, *noobaa.Spec.DBStorageClass, fmt.Sprintf("%s-ceph-rbd", c.sc.Name))
		noobaaplacement := getPlacement(&c.sc, "noobaa-core")
		assert.Equal(t, noobaa.Spec.Tolerations, noobaaplacement.Tolerations)
		assert.Equal(t, noobaa.Spec.Affinity, &corev1.Affinity{NodeAffinity: noobaaplacement.NodeAffinity})
		assert.Equal(t, *noobaa.Spec.PVPoolDefaultStorageClass, fmt.Sprintf("%s-ceph-rbd", c.sc.Name))
		assert.Equalf(t, noobaa.Namespace, c.sc.Namespace, "[%s] namespace mismatch", c.label)
		if c.envCore != "" {
			assert.Equalf(t, *noobaa.Spec.Image, c.envCore, "[%s] core envVar not applied to noobaa spec", c.label)
		}
		if c.envDB != "" {
			assert.Equalf(t, *noobaa.Spec.DBImage, c.envDB, "[%s] db envVar not applied to noobaa spec", c.label)
		}
	}
}

func TestNoobaaSystemInExternalClusterMode(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	reconciler := createExternalClusterReconciler(t)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertNoobaaResource(t, reconciler)
}

func assertNoobaaResource(t *testing.T, reconciler ReconcileStorageCluster) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	cr := &v1.StorageCluster{}
	err := reconciler.client.Get(nil, request.NamespacedName, cr)
	assert.NoError(t, err)

	// get the ceph cluster
	request.Name = generateNameForCephCluster(cr)
	foundCeph := &cephv1.CephCluster{}
	err = reconciler.client.Get(nil, request.NamespacedName, foundCeph)
	assert.NoError(t, err)

	// set the state to 'ClusterStateConnecting' (to mock a state where external cluster is still trying to connect)
	foundCeph.Status.State = cephv1.ClusterStateConnecting
	err = reconciler.client.Update(nil, foundCeph)
	assert.NoError(t, err)
	// calling 'ensureNoobaaSystem()' function and the expectation is that 'Noobaa' system is not be created
	err = reconciler.ensureNoobaaSystem(cr, reconciler.reqLogger)
	assert.NoError(t, err)
	fNoobaa := &v1alpha1.NooBaa{}
	request.Name = "noobaa"
	// expectation is not to get any Noobaa object
	err = reconciler.client.Get(nil, request.NamespacedName, fNoobaa)
	assert.Error(t, err)

	// now setting the state to 'ClusterStateConnected' (to mock a successful external cluster connection)
	foundCeph.Status.State = cephv1.ClusterStateConnected
	err = reconciler.client.Update(nil, foundCeph)
	assert.NoError(t, err)
	// call 'ensureNoobaaSystem()' to make sure it takes appropriate action
	// when ceph cluster is connected to an external cluster
	err = reconciler.ensureNoobaaSystem(cr, reconciler.reqLogger)
	assert.NoError(t, err)
	fNoobaa = &v1alpha1.NooBaa{}
	request.Name = "noobaa"
	// expectation is to get an appropriate Noobaa object
	err = reconciler.client.Get(nil, request.NamespacedName, fNoobaa)
	assert.NoError(t, err)
	assert.NotEmpty(t, fNoobaa.Labels[externalRgwEndpointLabelName])
	// The endpoint is base64 encoded, the decoded value is "10.20.30.40:50"
	assert.Equal(t, fNoobaa.Labels[externalRgwEndpointLabelName], "MTAuMjAuMzAuNDA6NTA=")
}

func getReconciler(t *testing.T, objs ...runtime.Object) ReconcileStorageCluster {
	registerObjs := []runtime.Object{&v1.StorageCluster{}}
	registerObjs = append(registerObjs, objs...)
	v1.SchemeBuilder.Register(registerObjs...)

	scheme, err := v1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = cephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1 scheme")
	}
	err = openshiftv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add openshiftv1 scheme")
	}
	client := fake.NewFakeClientWithScheme(scheme, registerObjs...)

	return ReconcileStorageCluster{
		scheme:   scheme,
		client:   client,
		platform: &CloudPlatform{},
	}
}
