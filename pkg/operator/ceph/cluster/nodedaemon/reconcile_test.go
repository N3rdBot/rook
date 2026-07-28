/*
Copyright 2024 The Rook Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodedaemon

import (
	"testing"

	"github.com/rook/rook/pkg/operator/ceph/cluster/mgr"
	"github.com/rook/rook/pkg/operator/ceph/cluster/mon"
	"github.com/rook/rook/pkg/operator/ceph/cluster/osd"
	"github.com/rook/rook/pkg/operator/ceph/cluster/rbd"
	"github.com/rook/rook/pkg/operator/ceph/file/mds"
	"github.com/rook/rook/pkg/operator/ceph/file/mirror"
	"github.com/rook/rook/pkg/operator/ceph/object"
	"github.com/rook/rook/pkg/operator/k8sutil"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podOnNode(name, node, appName, namespace string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				k8sutil.AppAttr:     appName,
				k8sutil.ClusterAttr: namespace,
			},
		},
		Spec: corev1.PodSpec{NodeName: node},
	}
}

func TestHasCoreCephPodsOnNode(t *testing.T) {
	const nodeX = "node-x"

	t.Run("no pods on node returns false", func(t *testing.T) {
		pods := []corev1.Pod{
			podOnNode("rook-ceph-mon-a", "node-y", mon.AppName, "rook-ceph"),
			podOnNode("rook-ceph-mds-a", nodeX, mds.AppName, "rook-ceph"),
		}
		assert.False(t, hasCoreCephPodsOnNode(pods, nodeX))
	})

	t.Run("only MDS pod on node returns false", func(t *testing.T) {
		pods := []corev1.Pod{
			podOnNode("rook-ceph-mds-myfs-a-abc", nodeX, mds.AppName, "rook-ceph-cluster-a"),
		}
		assert.False(t, hasCoreCephPodsOnNode(pods, nodeX),
			"MDS-only nodes must NOT trigger node daemon creation; "+
				"otherwise duplicate ceph-exporter deployments can be "+
				"created when MDS lands on a node owned by another cluster")
	})

	t.Run("only fs-mirror pod on node returns false", func(t *testing.T) {
		pods := []corev1.Pod{
			podOnNode("rook-ceph-fs-mirror-a-abc", nodeX, mirror.AppName, "rook-ceph-cluster-a"),
		}
		assert.False(t, hasCoreCephPodsOnNode(pods, nodeX))
	})

	t.Run("MDS plus OSD on node returns true", func(t *testing.T) {
		pods := []corev1.Pod{
			podOnNode("rook-ceph-osd-0", nodeX, osd.AppName, "rook-ceph"),
			podOnNode("rook-ceph-mds-myfs-a", nodeX, mds.AppName, "rook-ceph"),
		}
		assert.True(t, hasCoreCephPodsOnNode(pods, nodeX))
	})

	cases := []struct {
		name    string
		appName string
		want    bool
	}{
		{"mon is core", mon.AppName, true},
		{"mgr is core", mgr.AppName, true},
		{"osd is core", osd.AppName, true},
		{"rgw (object) is core", object.AppName, true},
		{"rbd-mirror is core", rbd.AppName, true},
		{"mds is NOT core", mds.AppName, false},
		{"fs-mirror is NOT core", mirror.AppName, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pods := []corev1.Pod{
				podOnNode("pod", nodeX, tc.appName, "rook-ceph"),
			}
			assert.Equal(t, tc.want, hasCoreCephPodsOnNode(pods, nodeX), "app=%s", tc.appName)
		})
	}

	t.Run("missing app label returns false", func(t *testing.T) {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rogue",
				Namespace: "rook-ceph",
				Labels:    map[string]string{"unrelated": "label"},
			},
			Spec: corev1.PodSpec{NodeName: nodeX},
		}
		assert.False(t, hasCoreCephPodsOnNode([]corev1.Pod{pod}, nodeX))
	})
}

func TestHasCoreCephPodsOnNode_MultiNamespaceRegression(t *testing.T) {
	const nodeX = "node-x"

	pods := []corev1.Pod{
		podOnNode("rook-ceph-mds-myfs-a-abc", nodeX, mds.AppName, "rook-ceph-cluster-a"),
		podOnNode("rook-ceph-osd-0", nodeX, osd.AppName, "rook-ceph-cluster-b"),
		podOnNode("rook-ceph-mon-b", nodeX, mon.AppName, "rook-ceph-cluster-b"),
		podOnNode("rook-ceph-mgr-b", nodeX, mgr.AppName, "rook-ceph-cluster-b"),
	}

	clusterAPods := []corev1.Pod{pods[0]}
	assert.False(t, hasCoreCephPodsOnNode(clusterAPods, nodeX),
		"cluster-a (only MDS on node) must NOT deploy node daemons on node-x")

	clusterBPods := []corev1.Pod{pods[1], pods[2], pods[3]}
	assert.True(t, hasCoreCephPodsOnNode(clusterBPods, nodeX),
		"cluster-b (OSD/MON/MGR on node) must deploy node daemons on node-x")
}