package main

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// BrokerState mirrors the three keys the broker writes into its state Secret.
type BrokerState struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time // zero when the key is absent
}

// LoadClusterClient reads kubeconfig the same way kubectl does (KUBECONFIG env,
// then ~/.kube/config). When contextName is non-empty it overrides the current
// context.
func LoadClusterClient(contextName string) (kubernetes.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(restCfg)
}

// ReadBrokerState fetches the broker's state Secret and decodes the three keys
// the sync needs. A missing access_token or refresh_token is an error: the
// broker has not finished bootstrapping yet and the local Keychain must not be
// overwritten with a half-populated payload.
func ReadBrokerState(ctx context.Context, k kubernetes.Interface, namespace, name string) (*BrokerState, error) {
	sec, err := k.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	state := &BrokerState{
		AccessToken:  string(sec.Data["access_token"]),
		RefreshToken: string(sec.Data["refresh_token"]),
	}
	if rawExp, ok := sec.Data["expires_at"]; ok && len(rawExp) > 0 {
		t, err := time.Parse(time.RFC3339, string(rawExp))
		if err != nil {
			return nil, fmt.Errorf("parse expires_at %q: %w", rawExp, err)
		}
		state.ExpiresAt = t
	}
	if state.AccessToken == "" || state.RefreshToken == "" {
		return nil, fmt.Errorf("secret %s/%s missing access_token or refresh_token (broker may not have reloaded yet)", namespace, name)
	}
	return state, nil
}
