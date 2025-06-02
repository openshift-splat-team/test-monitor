package controller

import (
	"context"
	"fmt"
	"log"
	"path"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift-splat-team/vsphere-capacity-manager/pkg/utils"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
)

const (
	BoskosIdLabel = "boskos-lease-id"
)

type LeaseReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	RESTMapper     meta.RESTMapper
	UncachedClient client.Client

	// Namespace is the namespace in which the ControlPlaneMachineSet controller should operate.
	// Any ControlPlaneMachineSet not in this namespace should be ignored.
	Namespace string

	// OperatorName is the name of the ClusterOperator with which the controller should report
	// its status.
	OperatorName string

	// ReleaseVersion is the version of current cluster operator release.
	ReleaseVersion string
}

func (l *LeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Lease{}).
		Complete(l); err != nil {
		return fmt.Errorf("error setting up controller: %w", err)
	}

	// Set up API helpers from the manager.
	l.Client = mgr.GetClient()
	l.Scheme = mgr.GetScheme()
	l.Recorder = mgr.GetEventRecorderFor("leases-controller")
	l.RESTMapper = mgr.GetRESTMapper()
	leases = make(map[string]*v1.Lease)
	pools = make(map[string]*v1.Pool)
	networks = make(map[string]*v1.Network)
	return nil
}

// getAvailableNetworks retrieves networks which are not owned by a lease
func (l *LeaseReconciler) getAvailableNetworks(pool *v1.Pool, networkType v1.NetworkType) []*v1.Network {
	networksInPool := make(map[string]*v1.Network)
	availableNetworks := make([]*v1.Network, 0)
	for _, portGroupPath := range pool.Spec.Topology.Networks {
		_, networkName := path.Split(portGroupPath)

		for _, network := range networks {
			if (*network.Spec.PodName == pool.Spec.IBMPoolSpec.Pod) &&
				(network.Spec.PortGroupName == networkName) {
				networksInPool[network.Name] = network
				break
			}
		}
	}

	for _, network := range networksInPool {
		hasOwner := false
		for _, lease := range leases {
			for _, ownerRef := range lease.OwnerReferences {
				if ownerRef.Name == network.Name &&
					ownerRef.Kind == network.Kind {
					hasOwner = true
					break
				}
			}
			if hasOwner {
				break
			}
		}

		thisNetworkType := string(v1.NetworkTypeSingleTenant)
		if network.ObjectMeta.Labels != nil {
			if val, exists := network.ObjectMeta.Labels[v1.NetworkTypeLabel]; exists {
				log.Printf("network found with NeworkTypeLabel: %s", val)
				thisNetworkType = val
			}
		}
		if thisNetworkType != string(networkType) {
			continue
		}
		if !hasOwner {
			availableNetworks = append(availableNetworks, network)
		}
	}
	return availableNetworks
}

func getIBMDatacenterAndPod(server string) (string, string) {
	for _, pool := range pools {
		if pool.Spec.Server == server {
			return pool.Spec.IBMPoolSpec.Datacenter, pool.Spec.IBMPoolSpec.Pod
		}
	}
	return "", ""
}

// reconcilePoolStates updates the states of all pools. this ensures we have the most up-to-date state of the pools
// before we attempt to reconcile any leases. the pool resource statuses are not updated.
func reconcilePoolStates() []*v1.Pool {
	var outList []*v1.Pool

	networksInUse := make(map[string]map[string]string)

	for poolName, pool := range pools {
		vcpus := 0
		memory := 0

		for _, lease := range leases {
			for _, ownerRef := range lease.OwnerReferences {
				if ownerRef.Kind == pool.Kind && ownerRef.Name == pool.Name {
					vcpus += lease.Spec.VCpus
					memory += lease.Spec.Memory

					var serverNetworks map[string]string
					var exists bool

					dc, pod := getIBMDatacenterAndPod(lease.Status.Server)
					dcId := fmt.Sprintf("dcid-%s-%s", dc, pod)
					if serverNetworks, exists = networksInUse[dcId]; !exists {
						serverNetworks = make(map[string]string)
						networksInUse[dcId] = serverNetworks
					}

					for _, networkPath := range lease.Status.Topology.Networks {
						_, networkName := path.Split(networkPath)
						serverNetworks[networkName] = networkName
					}
					break
				}
			}
		}

		overCommitRatio, err := strconv.ParseFloat(pool.Spec.OverCommitRatio, 32)
		if err != nil {
			log.Printf("error converting overCommitRatio to float %v setting to 1.0", err)
			overCommitRatio = 1.0
		}

		pool.Status.VCpusAvailable = int(float64(pool.Spec.VCpus)*overCommitRatio) - vcpus
		pool.Status.MemoryAvailable = pool.Spec.Memory - memory

		pools[poolName] = pool
		outList = append(outList, pool)
	}

	for _, pool := range outList {
		availableNetworks := 0
		for _, network := range pool.Spec.Topology.Networks {
			_, networkName := path.Split(network)
			dcId := fmt.Sprintf("dcid-%s-%s", pool.Spec.IBMPoolSpec.Datacenter, pool.Spec.IBMPoolSpec.Pod)
			serverNetworks := networksInUse[dcId]
			if _, ok := serverNetworks[networkName]; !ok {
				availableNetworks++
			}
		}
		pool.Status.NetworkAvailable = availableNetworks
	}

	return outList
}

func (l *LeaseReconciler) triggerPoolUpdates(ctx context.Context) {
	for _, pool := range pools {

		err := l.Client.Get(ctx, types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, pool)
		if err != nil {
			log.Printf("error getting pool %s: %v", pool.Name, err)
			continue
		}

		if pool.Annotations == nil {
			pool.Annotations = make(map[string]string)
		}

		pool.Annotations["last-updated"] = time.Now().Format(time.RFC3339)
		err = l.Client.Update(ctx, pool)
		if err != nil {
			log.Printf("error updating pool %s annotations: %v", pool.Name, err)
		}
	}
}

// returns a common portgroup that satisfies all known leases for this job. common port groups are scoped
// to a single vCenter. for multiple vCenters, a network lease for each vCenter will be claimed.
func (l *LeaseReconciler) getCommonNetworkForLease(lease *v1.Lease) (*v1.Network, error) {
	var exists bool
	var leaseID string

	if lease.Spec.VCpus == 0 && lease.Spec.Memory == 0 {
		return nil, fmt.Errorf("network-only lease %s", lease.Name)
	}
	if leaseID, exists = lease.Labels[BoskosIdLabel]; !exists {
		return nil, fmt.Errorf("no lease label found for %s", lease.Name)
	}

	for _, _lease := range leases {
		if _lease.Spec.VCpus == 0 && _lease.Spec.Memory == 0 {
			// this is a network-only lease. do not consider it.
			continue
		}
		if thisLeaseID, exists := _lease.Labels[BoskosIdLabel]; !exists {
			continue
		} else if thisLeaseID != leaseID {
			continue
		}
		for _, ownerRef := range _lease.OwnerReferences {
			if ownerRef.Kind != "Network" {
				continue
			}
			for _, network := range networks {
				if network.Name == ownerRef.Name && network.UID == ownerRef.UID {
					return network, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no common network found for %s", lease.Name)
}

func (l *LeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	reconcileLock.Lock()
	defer reconcileLock.Unlock()

	log.Print("Reconciling lease")
	defer log.Print("Finished reconciling lease")

	leaseKey := fmt.Sprintf("%s/%s", req.Namespace, req.Name)
	// Fetch the Lease instance.
	lease := &v1.Lease{}
	if err := l.Get(ctx, req.NamespacedName, lease); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if len(lease.Status.Phase) == 0 {
		lease.Status.Phase = v1.PHASE_PENDING
		lease.Status.Topology.Datacenter = "pending"
		lease.Status.Topology.Datastore = "/pending/datastore/pending"
		lease.Status.Topology.ComputeCluster = "/pending/host/pending"
		lease.Status.Server = "pending"
		lease.Status.Zone = "pending"
		lease.Status.Region = "pending"
		lease.Status.Name = "pending"
		lease.Status.ShortName = "pending"
		lease.Status.Topology.Networks = append(lease.Status.Topology.Networks, "/pending/network/pending")
		if err := l.Status().Update(ctx, lease); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to set the initial status on the lease %s: %w", lease.Name, err)
		}
	}

	if lease.Finalizers == nil {
		log.Print("setting finalizer on lease")
		lease.Finalizers = []string{v1.LeaseFinalizer}
		err := l.Client.Update(ctx, lease)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error setting lease finalizer: %w", err)
		}
	}

	promLabels := make(prometheus.Labels)
	promLabels["namespace"] = req.Namespace

	if lease.DeletionTimestamp != nil {
		log.Printf("lease %s is being deleted at %s", lease.Name, lease.DeletionTimestamp.String())

		// preserve finalizers not associated with VCM
		if lease.Finalizers != nil {
			var preservedFinalizers []string

			for _, finalizer := range lease.Finalizers {
				if finalizer == v1.LeaseFinalizer {
					continue
				}

				preservedFinalizers = append(preservedFinalizers, finalizer)
			}
			lease.Finalizers = preservedFinalizers
		}

		err := l.Update(ctx, lease)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error dropping finalizers from lease: %w", err)
		}

		if ownRef := utils.DoesLeaseHavePool(lease); ownRef != nil {
			promLabels["pool"] = ownRef.Name
		}

		delete(leases, leaseKey)
		if len(promLabels) >= 2 {
			LeasesInUse.With(promLabels).Dec()
		}
		reconcilePoolStates()
		l.triggerPoolUpdates(ctx)
		return ctrl.Result{}, nil
	}

	leases[leaseKey] = lease

	if lease.Status.Phase == v1.PHASE_FULFILLED {
		log.Print("lease is already fulfilled")
		return ctrl.Result{}, nil
	}

	updatedPools := reconcilePoolStates()

	lease.Status.Phase = v1.PHASE_PENDING

	pool := &v1.Pool{}
	if ref := utils.DoesLeaseHavePool(lease); ref == nil {
		pool, err = utils.GetPoolWithStrategy(lease, updatedPools, v1.RESOURCE_ALLOCATION_STRATEGY_UNDERUTILIZED)
		if err != nil {
			if l.Client.Status().Update(ctx, lease) != nil {
				log.Printf("unable to update lease: %v", err)
			}

			return ctrl.Result{}, fmt.Errorf("unable to get matching pool: %v", err)
		}
	} else {
		err = l.Get(ctx, types.NamespacedName{
			Namespace: req.Namespace,
			Name:      ref.Name,
		}, pool)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error getting pool: %v", err)
		}
	}

	var network *v1.Network

	if !utils.DoesLeaseHaveNetworks(lease) {
		var availableNetworks []*v1.Network
		network, err = l.getCommonNetworkForLease(lease)
		if err != nil {
			log.Printf("error getting common network for lease, will attempt to allocate a new one: %v", err)

			if len(lease.Spec.NetworkType) == 0 {
				lease.Spec.NetworkType = v1.NetworkTypeSingleTenant
			}

			availableNetworks = l.getAvailableNetworks(pool, lease.Spec.NetworkType)

			if lease.Spec.NetworkType == v1.NetworkTypeMultiTenant {
				// for mutli-tenant leases, there is no reason they can't fall back to single-tenant if there aren't
				// any multi-tenant leases available.
				availableNetworks = append(availableNetworks, l.getAvailableNetworks(pool, v1.NetworkTypeSingleTenant)...)
			}
		} else {
			availableNetworks = []*v1.Network{network}
		}

		log.Printf("available networks: %d - lease %s requested networks: %d", len(availableNetworks), lease.Name, lease.Spec.Networks)
		if len(availableNetworks) < lease.Spec.Networks {
			lease.OwnerReferences = nil
			return ctrl.Result{}, fmt.Errorf("lease requires %d networks, %d networks available", lease.Spec.Networks, len(availableNetworks))
		}

		var networks []string
		for idx := 0; idx < lease.Spec.Networks; idx++ {
			network = availableNetworks[idx]
			lease.OwnerReferences = append(lease.OwnerReferences, metav1.OwnerReference{
				APIVersion: network.APIVersion,
				Kind:       network.Kind,
				Name:       network.Name,
				UID:        network.UID,
			})
			networks = append(networks, fmt.Sprintf("/%s/network/%s", lease.Status.Topology.Datacenter, network.Spec.PortGroupName))
		}
		if len(networks) > 1 {
			log.Printf("%s requested more than one network", lease.Name)
		}
		lease.Status.Topology.Networks = networks
	}

	err = utils.GenerateEnvVars(lease, pool, network)
	if err != nil {
		log.Printf("error generating env vars: %v", err)
	}

	leaseStatus := lease.Status.DeepCopy()
	err = l.Client.Update(ctx, lease)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error updating lease, requeuing: %v", err)
	}

	leaseStatus.DeepCopyInto(&lease.Status)
	lease.Status.Phase = v1.PHASE_FULFILLED
	err = l.Client.Status().Update(ctx, lease)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error updating lease, requeuing: %v", err)
	}

	promLabels["pool"] = pool.Name
	LeasesInUse.With(promLabels).Add(1)

	if pool.Annotations == nil {
		pool.Annotations = make(map[string]string)
	}

	l.triggerPoolUpdates(ctx)

	return ctrl.Result{}, nil
}
