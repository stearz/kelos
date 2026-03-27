package helmchart

import (
	"strings"
	"testing"

	"github.com/kelos-dev/kelos/internal/manifests"
	sigyaml "sigs.k8s.io/yaml"
)

func TestRender_NilValues(t *testing.T) {
	data, err := Render(manifests.ChartFS, nil)
	if err != nil {
		t.Fatalf("rendering chart with nil values: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty rendered output")
	}
	output := string(data)
	for _, expected := range []string{
		"kind: Namespace",
		"kind: ServiceAccount",
		"kind: ClusterRole",
		"kind: Deployment",
		"kind: CronJob",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected rendered output to contain %q", expected)
		}
	}
	// NetworkPolicy should not be rendered by default (disabled)
	if strings.Contains(output, "kind: NetworkPolicy") {
		t.Error("expected no NetworkPolicy when networkpolicies.enabled is false by default")
	}
	if !strings.Contains(output, ":latest") {
		t.Error("expected :latest tags in rendered output when using default values")
	}
}

func TestRender_DefaultValues(t *testing.T) {
	vals := map[string]interface{}{
		"image": map[string]interface{}{
			"tag": "v0.0.0-test",
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty rendered output")
	}
	output := string(data)
	for _, expected := range []string{
		"kind: Namespace",
		"kind: ServiceAccount",
		"kind: ClusterRole",
		"kind: Deployment",
		"kind: CronJob",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected rendered output to contain %q", expected)
		}
	}
	// NetworkPolicy should not be rendered when not explicitly enabled
	if strings.Contains(output, "kind: NetworkPolicy") {
		t.Error("expected no NetworkPolicy when networkpolicies.enabled is not set")
	}
}

func TestRender_VersionOverride(t *testing.T) {
	vals := map[string]interface{}{
		"image": map[string]interface{}{
			"tag": "v1.2.3",
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	if strings.Contains(output, ":latest") {
		t.Error("expected no :latest tags in rendered output")
	}
	if !strings.Contains(output, ":v1.2.3") {
		t.Error("expected :v1.2.3 tags in rendered output")
	}
}

func TestRender_PullPolicy(t *testing.T) {
	vals := map[string]interface{}{
		"image": map[string]interface{}{
			"tag":        "latest",
			"pullPolicy": "IfNotPresent",
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "imagePullPolicy: IfNotPresent") {
		t.Error("expected imagePullPolicy: IfNotPresent in rendered output")
	}
}

func TestRender_DisableTelemetry(t *testing.T) {
	vals := map[string]interface{}{
		"telemetry": map[string]interface{}{
			"enabled": false,
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	if strings.Contains(output, "kelos-telemetry") {
		t.Error("expected kelos-telemetry CronJob to be excluded")
	}
}

func TestRender_ResourceOrdering(t *testing.T) {
	data, err := Render(manifests.ChartFS, nil)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	// Namespace must appear before Deployment and CronJob so that the
	// namespace exists when namespaced resources are applied.
	nsIdx := strings.Index(output, "kind: Namespace")
	deployIdx := strings.Index(output, "kind: Deployment")
	cronIdx := strings.Index(output, "kind: CronJob")
	if nsIdx < 0 || deployIdx < 0 || cronIdx < 0 {
		t.Fatal("expected Namespace, Deployment, and CronJob in rendered output")
	}
	if nsIdx >= deployIdx {
		t.Error("expected Namespace to appear before Deployment")
	}
	if nsIdx >= cronIdx {
		t.Error("expected Namespace to appear before CronJob")
	}
}

func TestRender_NetworkPoliciesDisabledByDefault(t *testing.T) {
	data, err := Render(manifests.ChartFS, nil)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	if strings.Contains(output, "kind: NetworkPolicy") {
		t.Error("expected no NetworkPolicy resources when networkpolicies.enabled is false")
	}
}

func TestRender_NetworkPoliciesCanBeEnabled(t *testing.T) {
	vals := map[string]interface{}{
		"networkpolicies": map[string]interface{}{
			"enabled": true,
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	// Should have both default-deny and controller-allow policies
	if !strings.Contains(output, "name: kelos-default-deny") {
		t.Error("expected kelos-default-deny NetworkPolicy")
	}
	if !strings.Contains(output, "name: kelos-controller-allow") {
		t.Error("expected kelos-controller-allow NetworkPolicy")
	}
}

func TestRender_NetworkPoliciesWithCustomRules(t *testing.T) {
	vals := map[string]interface{}{
		"networkpolicies": map[string]interface{}{
			"enabled": true,
			"rules": map[string]interface{}{
				"ingress": []map[string]interface{}{
					{
						"from": []map[string]interface{}{
							{
								"namespaceSelector": map[string]interface{}{
									"matchLabels": map[string]string{
										"custom": "label",
									},
								},
							},
						},
						"ports": []map[string]interface{}{
							{
								"protocol": "TCP",
								"port":     9999,
							},
						},
					},
				},
			},
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "port: 9999") {
		t.Error("expected custom port 9999 in NetworkPolicy rules")
	}
	if !strings.Contains(output, "custom: label") {
		t.Error("expected custom label in NetworkPolicy rules")
	}
}

func TestRender_ParseableOutput(t *testing.T) {
	vals := map[string]interface{}{
		"image": map[string]interface{}{
			"tag": "v0.0.0-test",
		},
	}
	data, err := Render(manifests.ChartFS, vals)
	if err != nil {
		t.Fatalf("rendering chart: %v", err)
	}
	// Verify each non-empty YAML document is actually parseable.
	docs := strings.Split(string(data), "---\n")
	validDocs := 0
	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if len(trimmed) == 0 {
			continue
		}
		var obj map[string]interface{}
		if err := sigyaml.Unmarshal([]byte(trimmed), &obj); err != nil {
			t.Errorf("invalid YAML document: %v\n---\n%s", err, trimmed)
		}
		validDocs++
	}
	if validDocs == 0 {
		t.Fatal("expected at least one valid YAML document in rendered output")
	}
}
