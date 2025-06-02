package data

import (
	corev1 "k8s.io/api/core/v1"
)

type TestContext struct {
	Namespace corev1.Namespace
	Failed    bool

	Pool        string
	NetworkType string
	Portgroup   string
}
