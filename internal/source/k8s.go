package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/blackhorseya/env-diff/internal/diff"
	"github.com/blackhorseya/env-diff/internal/envfile"
)

// k8sSource is a ConfigMap or Secret read through kubectl, addressed as
// k8s://<namespace>/<configmap|secret>/<name>. kubectl's own configuration
// picks the cluster and credentials (KUBECONFIG, the current context, exec
// plugins), so env-diff needs nothing beyond what kubectl already has.
type k8sSource struct {
	arg       string
	namespace string
	kind      string // "configmap" or "secret"
	name      string
	resolver  Resolver
}

func parseK8s(x Resolver, arg, rest string) (Source, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, fmt.Errorf("%s: want k8s://<namespace>/<configmap|secret>/<name>", arg)
	}
	kind := parts[1]
	if kind != "configmap" && kind != "secret" {
		return nil, fmt.Errorf("%s: unknown kind %q (want configmap or secret)", arg, kind)
	}
	return k8sSource{arg: arg, namespace: parts[0], kind: kind, name: parts[2], resolver: x}, nil
}

func (x k8sSource) Name() string {
	return x.arg
}

// Load runs kubectl get and reads the object's data. Secret data and
// ConfigMap binaryData are base64 on the wire and are decoded, so values
// compare as the pods see them.
func (x k8sSource) Load(c context.Context) (diff.Vars, error) {
	out, err := x.resolver.runner()(c, x.resolver.stderr(),
		"kubectl", "get", x.kind, x.name, "-n", x.namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", x.arg, err)
	}
	var obj struct {
		Data       map[string]string `json:"data"`
		BinaryData map[string]string `json:"binaryData"`
	}
	// The decode error is not wrapped: it could quote the output.
	if err := json.Unmarshal(out, &obj); err != nil {
		return nil, fmt.Errorf("%s: kubectl output is not valid JSON", x.arg)
	}

	vars := make(map[string]string, len(obj.Data)+len(obj.BinaryData))
	encoded := obj.BinaryData
	if x.kind == "secret" {
		encoded = obj.Data
	} else {
		maps.Copy(vars, obj.Data)
	}
	for key, value := range encoded {
		if _, dup := vars[key]; dup {
			return nil, fmt.Errorf("%s: key %s is in both data and binaryData", x.arg, key)
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("%s: key %s is not valid base64", x.arg, key)
		}
		vars[key] = string(decoded)
	}
	return envfile.FromMap(vars), nil
}
