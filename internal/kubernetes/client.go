// Package kubernetes provides a GET-only client and informer cache.
package kubernetes

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type readOnlyTransport struct{ base http.RoundTripper }

var allowedReadResources = map[string]map[string]bool{
	"": {
		"events": true, "namespaces": true, "nodes": true, "persistentvolumeclaims": true,
		"persistentvolumes": true, "pods": true,
	},
	"apps":           {"daemonsets": true, "deployments": true, "replicasets": true, "statefulsets": true},
	"batch":          {"cronjobs": true, "jobs": true},
	"policy":         {"poddisruptionbudgets": true},
	"storage.k8s.io": {"csinodes": true, "storageclasses": true, "volumeattachments": true},
}

// allowedReadPath is a second safety boundary beyond RBAC. It permits only the
// exact inventory resource families guard9s is designed to collect and rejects
// subresources such as pods/log, deployments/scale and pods/status. Names and
// namespaces are never included in transport errors.
func allowedReadPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	group, resourceIndex := "", -1
	switch {
	case len(parts) >= 3 && parts[0] == "api" && parts[1] == "v1":
		resourceIndex = 2
	case len(parts) >= 4 && parts[0] == "apis" && parts[2] == "v1":
		group, resourceIndex = parts[1], 3
	default:
		return false
	}
	resource := parts[resourceIndex]
	tail := len(parts) - resourceIndex - 1
	// Namespaced resources insert /namespaces/{namespace}/ before the actual
	// resource. /api/v1/namespaces/{name} itself remains a Namespace GET.
	if resource == "namespaces" && tail >= 2 {
		resourceIndex += 2
		if resourceIndex >= len(parts) {
			return false
		}
		resource = parts[resourceIndex]
		tail = len(parts) - resourceIndex - 1
	}
	if !allowedReadResources[group][resource] {
		return false
	}
	return tail <= 1
}

func (r readOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, errors.New("guard9s read-only transport rejected a non-GET request")
	}
	if !allowedReadPath(req.URL.Path) {
		return nil, errors.New("guard9s read-only transport rejected an out-of-scope resource request")
	}
	return r.base.RoundTrip(req)
}

// WrappedRoundTripper preserves client-go timeout/cancellation traversal.
func (r readOnlyTransport) WrappedRoundTripper() http.RoundTripper { return r.base }

// Protect rejects writes at the transport boundary, even if a future caller
// accidentally invokes a mutating method on the typed Kubernetes client.
func Protect(c *rest.Config) *rest.Config {
	c = rest.CopyConfig(c)
	c.Wrap(func(rt http.RoundTripper) http.RoundTripper { return readOnlyTransport{base: rt} })
	c.UserAgent = "guard9s/read-only"
	c.QPS = 10
	c.Burst = 20
	c.Timeout = 30 * time.Second
	return c
}

func Config(path, contextName string) (*rest.Config, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path != "" {
		rules.ExplicitPath = path
	}
	raw, err := rules.Load()
	if err != nil {
		return nil, "", errors.New("could not load kubeconfig; check the file path and permissions")
	}
	if contextName == "" {
		contextName = raw.CurrentContext
	}
	selected, ok := raw.Contexts[contextName]
	if !ok || selected == nil {
		return nil, "", errors.New("requested kubeconfig context does not exist")
	}
	if auth := raw.AuthInfos[selected.AuthInfo]; auth != nil && (auth.Exec != nil || auth.AuthProvider != nil) {
		return nil, "", errors.New("credential execution/auth-provider plugins are unsupported in V1; use a trusted, read-only credential kubeconfig")
	}
	loader := clientcmd.NewNonInteractiveClientConfig(*raw, contextName, &clientcmd.ConfigOverrides{}, rules)
	config, err := loader.ClientConfig()
	if err != nil {
		return nil, "", errors.New("invalid kubeconfig; validate cluster, credentials and context configuration")
	}
	if config.TLSClientConfig.Insecure {
		return nil, "", errors.New("insecure-skip-tls-verify is unsupported; configure the cluster CA")
	}
	return Protect(config), contextName, nil
}
func Client(path, contextName string) (kubernetes.Interface, string, error) {
	config, name, err := Config(path, contextName)
	if err != nil {
		return nil, "", err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", fmt.Errorf("could not initialize read-only Kubernetes client")
	}
	return client, name, nil
}
