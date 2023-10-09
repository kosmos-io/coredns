package kubernetes

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin/kubernetes/object"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const (
	ClusterGroup    = "kosmos.io"
	ClusterVersion  = "v1alpha1"
	ClusterResource = "clusters"
	ClusterKind     = "Cluster"

	ClusterNameIndex = "Name"
)

const (
	maxRetries = 10
)

type clusterController struct {
	ctx context.Context

	client dynamic.Interface
	queue  workqueue.RateLimitingInterface

	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	stopCh   chan struct{}

	dnsControllersLock      sync.RWMutex
	dnsControllers          map[string]*dnsControl
	dncControllersWaitGroup wait.Group
	dnsOpts                 dnsControlOpts

	clusterController cache.Controller
	clusterLister     cache.Indexer
}

func newClusterController(ctx context.Context, client dynamic.Interface, opts dnsControlOpts) *clusterController {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	c := clusterController{
		ctx:            ctx,
		stopCh:         make(chan struct{}),
		client:         client,
		queue:          queue,
		dnsOpts:        opts,
		dnsControllers: make(map[string]*dnsControl),
	}

	var identity object.ToFunc
	identity = func(obj interface{}) (interface{}, error) {
		return obj, nil
	}

	c.clusterLister, c.clusterController = object.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc:  clusterListFunc(ctx, c.client),
			WatchFunc: clusterWatchFunc(ctx, c.client),
		},
		&unstructured.Unstructured{},
		cache.ResourceEventHandlerFuncs{AddFunc: c.addCluster, UpdateFunc: c.updateCluster, DeleteFunc: c.deleteCluster},
		cache.Indexers{ClusterNameIndex: clusterNameIndexFunc},
		object.DefaultProcessor(identity, nil),
	)

	return &c
}

func (c *clusterController) ServiceList() []*object.Service {
	for _, ctrl := range c.dnsControllers {
		svcList := ctrl.ServiceList()
		if len(svcList) > 0 {
			return svcList
		}
	}
	return nil
}

func (c *clusterController) EndpointsList() []*object.Endpoints {
	for _, ctrl := range c.dnsControllers {
		epList := ctrl.EndpointsList()
		if len(epList) > 0 {
			return epList
		}
	}
	return nil
}

func (c *clusterController) SvcIndex(s string) []*object.Service {
	for _, ctrl := range c.dnsControllers {
		svcList := ctrl.SvcIndex(s)
		if len(svcList) > 0 {
			return svcList
		}
	}
	return nil
}

func (c *clusterController) SvcIndexReverse(s string) []*object.Service {
	for _, ctrl := range c.dnsControllers {
		svcList := ctrl.SvcIndexReverse(s)
		if len(svcList) > 0 {
			return svcList
		}
	}
	return nil
}

func (c *clusterController) PodIndex(s string) []*object.Pod {
	for _, ctrl := range c.dnsControllers {
		podList := ctrl.PodIndex(s)
		if len(podList) > 0 {
			return podList
		}
	}
	return nil
}

func (c *clusterController) EpIndex(s string) []*object.Endpoints {
	for _, ctrl := range c.dnsControllers {
		epList := ctrl.EpIndex(s)
		if len(epList) > 0 {
			return epList
		}
	}
	return nil
}

func (c *clusterController) NodeIndex(s string) []*api.Node {
	for _, ctrl := range c.dnsControllers {
		nodeList := ctrl.NodeIndex(s)
		if len(nodeList) > 0 {
			return nodeList
		}
	}
	return nil
}

func (c *clusterController) EpIndexReverse(s string) []*object.Endpoints {
	for _, ctrl := range c.dnsControllers {
		epList := ctrl.EpIndexReverse(s)
		if len(epList) > 0 {
			return epList
		}
	}
	return nil
}

func (c *clusterController) GetNodeByName(ctx context.Context, s string) (*api.Node, error) {
	for _, ctrl := range c.dnsControllers {
		node, _ := ctrl.GetNodeByName(ctx, s)
		if node != nil {
			return node, nil
		}
	}
	return nil, fmt.Errorf("node not found")
}

func (c *clusterController) GetNamespaceByName(s string) (*api.Namespace, error) {
	for _, ctrl := range c.dnsControllers {
		ns, _ := ctrl.GetNamespaceByName(s)
		if ns != nil {
			return ns, nil
		}
	}
	return nil, fmt.Errorf("namespace not found")
}

func (c *clusterController) Run() {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("starting cluster controller")
	defer log.Info("shutting down cluster controller")

	go c.clusterController.Run(c.stopCh)
	if !cache.WaitForCacheSync(c.stopCh, c.clusterController.HasSynced) {
		log.Errorf("failed to wait for caches to sync")
		return
	}

	go wait.Until(c.worker, time.Second, c.stopCh)
	<-c.stopCh
}

func (c *clusterController) HasSynced() bool {
	if !c.clusterController.HasSynced() {
		return false
	}

	clusters := c.clusterLister.List()
	if len(c.dnsControllers) != len(clusters) {
		return false
	}

	for cluster, ctrl := range c.dnsControllers {
		if !ctrl.HasSynced() {
			log.Infof("waiting for cluster %s to sync", cluster)
			return false
		}
	}

	return true
}

func (c *clusterController) Stop() error {
	c.stopLock.Lock()
	defer c.stopLock.Unlock()

	// Only try draining the workqueue if we haven't already.
	if !c.shutdown {
		close(c.stopCh)
		c.shutdown = true
		return nil
	}

	return fmt.Errorf("shutdown already in progress")
}

func (c *clusterController) Modified() int64 {
	var max int64
	for _, ctrl := range c.dnsControllers {
		modified := ctrl.Modified()
		if modified > max {
			max = modified
		}
	}
	return max
}

func (c *clusterController) addCluster(obj interface{}) {
	c.enqueue(obj)
}

func (c *clusterController) deleteCluster(obj interface{}) {
	c.enqueue(obj)
}

func (c *clusterController) updateCluster(older, newer interface{}) {
	oldObj := older.(*unstructured.Unstructured)
	newObj := newer.(*unstructured.Unstructured)

	var cidrsChanged bool
	oldCIDRsMap, _, err := unstructured.NestedStringMap(oldObj.Object, "spec", "globalCIDRsMap")
	newCIDRsMap, _, err := unstructured.NestedStringMap(oldObj.Object, "spec", "globalCIDRsMap")
	if err == nil && equality.Semantic.DeepEqual(oldCIDRsMap, newCIDRsMap) {
		cidrsChanged = true
	}

	var kubeconfigChanged bool
	oldConfig, _, err := unstructured.NestedString(oldObj.Object, "spec", "kubeconfig")
	newConfig, _, err := unstructured.NestedString(oldObj.Object, "spec", "kubeconfig")
	if err == nil && equality.Semantic.DeepEqual(oldConfig, newConfig) {
		cidrsChanged = true
	}

	if newObj.GetDeletionTimestamp().IsZero() &&
		!cidrsChanged &&
		!kubeconfigChanged {
		return
	}

	c.enqueue(newer)
}

func (c *clusterController) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}

	c.queue.Add(key)
}

func (c *clusterController) worker() {
	for c.processNextCluster() {
		select {
		case <-c.stopCh:
			return
		default:
		}
	}
}

func (c *clusterController) processNextCluster() bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	defer c.queue.Done(key)

	err := c.syncCluster(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *clusterController) cleanControllerByClusterName(clusterName string) error {
	c.dnsControllersLock.RLock()
	ctrl := c.dnsControllers[clusterName]
	c.dnsControllersLock.RUnlock()

	if ctrl != nil {
		err := ctrl.Stop()
		if err != nil {
			return fmt.Errorf("cannot stop dns controller, cluster: %s", clusterName)
		}

		c.dnsControllersLock.Lock()
		delete(c.dnsControllers, clusterName)
		c.dnsControllersLock.Unlock()
	}
	return nil
}

func (c *clusterController) syncCluster(key string) error {
	log.Infof("Start to sync cluster: %v", key)

	gvr := schema.GroupVersionResource{
		Group:    ClusterGroup,
		Version:  ClusterVersion,
		Resource: ClusterResource,
	}

	obj, exists, err := c.clusterLister.GetByKey(key)
	if !exists {
		log.Infof("cluster has been deleted, cluster: %s", key)
		return nil
	}
	if err != nil {
		log.Errorf("cannot get cluster by key: %v, err: %v", key, err)
		return nil
	}

	cluster, ok := obj.(*unstructured.Unstructured)
	if !ok {
		log.Errorf("failed to convert cluster")
		return nil
	}

	clusterName := cluster.GetName()

	if !cluster.GetDeletionTimestamp().IsZero() {
		log.Infof("try to stop cluster informer %s", cluster.GetName())

		if !ContainsFinalizer(cluster, corednsFinalizer) {
			return nil
		}

		err = c.cleanControllerByClusterName(clusterName)
		if err != nil {
			return err
		}

		RemoveFinalizer(cluster, corednsFinalizer)
		if _, err := c.client.Resource(gvr).Update(context.TODO(), cluster, meta.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to remove finalizer, err: %v", err)
		}

		log.Infof("successfully removed cluster %s", clusterName)

		return nil
	}

	if !ContainsFinalizer(cluster, corednsFinalizer) {
		AddFinalizer(cluster, corednsFinalizer)
		if _, err := c.client.Resource(gvr).Update(context.TODO(), cluster, meta.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to add finalizer, err: %v", err)
		}
	}

	err = c.cleanControllerByClusterName(clusterName)
	if err != nil {
		log.Warningf("clean controllers failed, cluster: %s", clusterName)
	}

	config, err := buildRESTConfig(cluster)
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("cannot get init clientset, err: %v", err)
	}

	dnsCtrl := newdnsController(c.ctx, client, c.dnsOpts, cluster)

	c.dncControllersWaitGroup.Start(dnsCtrl.Run)

	if !cache.WaitForCacheSync(c.stopCh, dnsCtrl.HasSynced) {
		return fmt.Errorf("failed to wait for cluster %s to sync", clusterName)
	}

	c.dnsControllersLock.Lock()
	c.dnsControllers[clusterName] = dnsCtrl
	c.dnsControllersLock.Unlock()

	log.Infof("cluster %s has synced", clusterName)

	return nil
}

func (c *clusterController) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		log.Info("Error syncing cluster, retrying.", "key: ", key, "error: ", err)
		c.queue.AddRateLimited(key)
		return
	}

	log.Info("Dropping cluster out of the queue", "key: ", key, "error: ", err)
	c.queue.Forget(key)
	utilruntime.HandleError(err)
}

func clusterNameIndexFunc(obj interface{}) ([]string, error) {
	o, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, errObj
	}
	return []string{o.GetName()}, nil
}

func clusterListFunc(ctx context.Context, c dynamic.Interface) func(meta.ListOptions) (runtime.Object, error) {
	return func(opts meta.ListOptions) (runtime.Object, error) {
		gvr := schema.GroupVersionResource{
			Group:    ClusterGroup,
			Version:  ClusterVersion,
			Resource: ClusterResource,
		}
		listV1, err := c.Resource(gvr).List(ctx, opts)
		return listV1, err
	}
}

func clusterWatchFunc(ctx context.Context, c dynamic.Interface) func(options meta.ListOptions) (watch.Interface, error) {
	return func(options meta.ListOptions) (watch.Interface, error) {
		gvr := schema.GroupVersionResource{
			Group:    ClusterGroup,
			Version:  ClusterVersion,
			Resource: ClusterResource,
		}
		w, err := c.Resource(gvr).Watch(ctx, options)
		return w, err
	}
}

func buildRESTConfig(cluster *unstructured.Unstructured) (*rest.Config, error) {
	kubeconfig, isString, err := unstructured.NestedString(cluster.Object, "spec", "kubeconfig")
	if !isString || err != nil {
		return nil, fmt.Errorf("cannot find kubeconfig by cluster : %s", cluster.GetName())
	}

	decodeConfig, err := base64.StdEncoding.DecodeString(kubeconfig)
	if !isString || err != nil {
		return nil, fmt.Errorf("failed to decode kubeconfig, cluster : %s, kubeconfig: %s", cluster.GetName(), kubeconfig)
	}

	clientConfig, err := clientcmd.NewClientConfigFromBytes(decodeConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot convert cluster kubeconfig, cluster: %s, kubeconfig: %s", cluster.GetName(), decodeConfig)
	}

	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot get rest config, err: %v", err)
	}

	return config, nil
}
