package ironcore

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
)

func TestNewCIDRRangeAllocator(t *testing.T) {
	var recorderProvider recorder.Provider
	rec := recorderProvider.GetEventRecorderFor("cidrAllocator")
	klog.V(0).Infof("Sending events to api server.")
	ref := &v1.ObjectReference{
		APIVersion: "v1",
		Kind:       "Node",
		Name:       "foo",
		UID:        "bla",
		Namespace:  "",
	}
	rec.Event(ref, v1.EventTypeNormal, "foo", "bar")
}
