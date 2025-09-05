package dnsnameresolver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"

	kubeovnapiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnclient "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	kubeovnclientv1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// DNSNameResolver is a plugin that looks up responses from other plugins
// and updates the status of DNSNameResolver objects.
type DNSNameResolver struct {
	Next plugin.Handler

	minimumTTL       int32
	failureThreshold int32

	// Data mapping for the regularDNSInfo and wildcardDNSInfo maps:
	// DNS name --> DNSNameResolver object name.
	// regularDNSInfo map is used for storing regular DNS name details.
	regularDNSInfo map[string]string
	// wildcardDNSInfo map is used for storing wildcard DNS name details.
	wildcardDNSInfo map[string]string
	// regularMapLock is used to serialize the access to the regularDNSInfo
	// map.
	regularMapLock sync.Mutex
	// wildcardMapLock is used to serialize the access to the wildcardDNSInfo
	// map.
	wildcardMapLock sync.Mutex

	// client and informer for handling DNSNameResolver objects.
	kubeovnClient           kubeovnclientv1.KubeovnV1Interface
	dnsNameResolverInformer cache.SharedIndexInformer
	stopCh                  chan struct{}
	stopLock                sync.Mutex
	shutdown                bool
}

// New returns an initialized DNSNameResolver with default settings.
func New() *DNSNameResolver {
	return &DNSNameResolver{
		regularDNSInfo:   make(map[string]string),
		wildcardDNSInfo:  make(map[string]string),
		minimumTTL:       defaultMinTTL,
		failureThreshold: defaultFailureThreshold,
	}
}

const (
	// defaultResyncPeriod gives the resync period used for creating the DNSNameResolver informer.
	defaultResyncPeriod = 24 * time.Hour
	// defaultMinTTL will be used when minTTL is not explicitly configured.
	defaultMinTTL int32 = 5
	// defaultFailureThreshold will be used when failureThreshold is not explicitly configured.
	defaultFailureThreshold int32 = 5
)

// initInformer initializes the DNSNameResolver informer.
func (resolver *DNSNameResolver) initInformer(networkClient kubeovnclient.Interface) (err error) {
	// Get the client for version v1alpha1 for DNSNameResolver objects.
	resolver.kubeovnClient = networkClient.KubeovnV1()

	// Create the DNSNameResolver informer.
	resolver.dnsNameResolverInformer = kubeovninformer.NewSharedInformerFactory(networkClient, defaultResyncPeriod).Kubeovn().V1().DNSNameResolvers().Informer()

	// Add the event handlers for Add, Delete and Update events.
	resolver.dnsNameResolverInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Add event.
		AddFunc: func(obj interface{}) {
			// Get the DNSNameResolver object.
			resolverObj, ok := obj.(*kubeovnapiv1.DNSNameResolver)
			if !ok {
				log.Infof("object not of type DNSNameResolver: %v", obj)
				return
			}

			dnsName := strings.ToLower(string(resolverObj.Spec.Name))
			// Check if the DNS name is wildcard or regular.
			if isWildcard(dnsName) {
				// If the DNS name is wildcard, add the details of the DNSNameResolver
				// object to the wildcardDNSInfo map.
				resolver.wildcardMapLock.Lock()
				if existingObjName, exists := resolver.wildcardDNSInfo[dnsName]; exists && existingObjName != resolverObj.Name {
					// If a different DNSNameResolver object already exists for this DNS name, skip
					resolver.wildcardMapLock.Unlock()
					return
				}
				resolver.wildcardDNSInfo[dnsName] = resolverObj.Name
				resolver.wildcardMapLock.Unlock()
			} else {
				// If the DNS name is regular, add the details of the DNSNameResolver
				// object to the regularDNSInfo map.
				resolver.regularMapLock.Lock()
				if existingObjName, exists := resolver.regularDNSInfo[dnsName]; exists && existingObjName != resolverObj.Name {
					// If a different DNSNameResolver object already exists for this DNS name, skip
					resolver.regularMapLock.Unlock()
					return
				}
				resolver.regularDNSInfo[dnsName] = resolverObj.Name
				resolver.regularMapLock.Unlock()
			}
		},
		// Update event.
		UpdateFunc: func(oldObj, newObj interface{}) {
			// Get the old and new DNSNameResolver objects.
			oldResolverObj, oldOk := oldObj.(*kubeovnapiv1.DNSNameResolver)
			newResolverObj, newOk := newObj.(*kubeovnapiv1.DNSNameResolver)
			if !oldOk || !newOk {
				log.Infof("objects not of type DNSNameResolver: old=%v, new=%v", oldObj, newObj)
				return
			}

			oldDNSName := strings.ToLower(string(oldResolverObj.Spec.Name))
			newDNSName := strings.ToLower(string(newResolverObj.Spec.Name))

			// If the DNS name hasn't changed, no need to update mappings
			if oldDNSName == newDNSName {
				return
			}

			// Remove old mapping
			if isWildcard(oldDNSName) {
				resolver.wildcardMapLock.Lock()
				if existingObjName, exists := resolver.wildcardDNSInfo[oldDNSName]; exists && existingObjName == oldResolverObj.Name {
					delete(resolver.wildcardDNSInfo, oldDNSName)
				}
				resolver.wildcardMapLock.Unlock()
			} else {
				resolver.regularMapLock.Lock()
				if existingObjName, exists := resolver.regularDNSInfo[oldDNSName]; exists && existingObjName == oldResolverObj.Name {
					delete(resolver.regularDNSInfo, oldDNSName)
				}
				resolver.regularMapLock.Unlock()
			}

			// Add new mapping
			if isWildcard(newDNSName) {
				resolver.wildcardMapLock.Lock()
				if existingObjName, exists := resolver.wildcardDNSInfo[newDNSName]; exists && existingObjName != newResolverObj.Name {
					// If a different DNSNameResolver object already exists for this DNS name, skip
					resolver.wildcardMapLock.Unlock()
					return
				}
				resolver.wildcardDNSInfo[newDNSName] = newResolverObj.Name
				resolver.wildcardMapLock.Unlock()
			} else {
				resolver.regularMapLock.Lock()
				if existingObjName, exists := resolver.regularDNSInfo[newDNSName]; exists && existingObjName != newResolverObj.Name {
					// If a different DNSNameResolver object already exists for this DNS name, skip
					resolver.regularMapLock.Unlock()
					return
				}
				resolver.regularDNSInfo[newDNSName] = newResolverObj.Name
				resolver.regularMapLock.Unlock()
			}
		},
		// Delete event.
		DeleteFunc: func(obj interface{}) {
			// Get the DNSNameResolver object.
			resolverObj, ok := obj.(*kubeovnapiv1.DNSNameResolver)
			if !ok {
				log.Infof("object not of type DNSNameResolver: %v", obj)
				return
			}

			dnsName := strings.ToLower(string(resolverObj.Spec.Name))
			// Check if the DNS name is wildcard or regular.
			if isWildcard(dnsName) {
				// If the DNS name is wildcard, delete the details of the DNSNameResolver
				// object from the wildcardDNSInfo map.
				resolver.wildcardMapLock.Lock()
				if existingObjName, exists := resolver.wildcardDNSInfo[dnsName]; exists && existingObjName == resolverObj.Name {
					delete(resolver.wildcardDNSInfo, dnsName)
				}
				resolver.wildcardMapLock.Unlock()
			} else {
				// If the DNS name is regular, delete the details of the DNSNameResolver
				// object from the regularDNSInfo map.
				resolver.regularMapLock.Lock()
				if existingObjName, exists := resolver.regularDNSInfo[dnsName]; exists && existingObjName == resolverObj.Name {
					delete(resolver.regularDNSInfo, dnsName)
				}
				resolver.regularMapLock.Unlock()
			}
		},
	})
	return nil
}

// initPlugin initializes the ocp_dnsnameresolver plugin and returns the plugin startup and
// shutdown callback functions.
func (resolver *DNSNameResolver) initPlugin() (func() error, func() error, error) {
	// Create a client supporting network.openshift.io apis.
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, err
	}

	networkClient, err := kubeovnclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	err = resolver.initInformer(networkClient)
	if err != nil {
		return nil, nil, err
	}

	resolver.stopCh = make(chan struct{})

	onStart := func() error {
		go func() {
			resolver.dnsNameResolverInformer.Run(resolver.stopCh)
		}()

		timeout := 5 * time.Second
		timeoutTicker := time.NewTicker(timeout)
		defer timeoutTicker.Stop()
		logDelay := 500 * time.Millisecond
		logTicker := time.NewTicker(logDelay)
		defer logTicker.Stop()
		checkSyncTicker := time.NewTicker(100 * time.Millisecond)
		defer checkSyncTicker.Stop()
		for {
			select {
			case <-checkSyncTicker.C:
				if resolver.dnsNameResolverInformer.HasSynced() {
					return nil
				}
			case <-logTicker.C:
				log.Info("waiting for DNS Name Resolver Informer sync before starting server")
			case <-timeoutTicker.C:
				// Following similar strategy of the kubernetes CoreDNS plugin to start the server
				// with unsynced informer. For reference:
				// https://github.com/openshift/coredns/blob/022a0530038602605b8f3e8866c2a6ded97708cc/plugin/kubernetes/kubernetes.go#L261-L287
				log.Warning("starting server with unsynced DNS Name Resolver Informer")
				return nil
			}
		}
	}

	onShut := func() error {
		resolver.stopLock.Lock()
		defer resolver.stopLock.Unlock()

		// Only try draining the workqueue if we haven't already.
		if !resolver.shutdown {
			close(resolver.stopCh)
			resolver.shutdown = true

			return nil
		}

		return fmt.Errorf("shutdown already in progress")
	}

	return onStart, onShut, nil
}
