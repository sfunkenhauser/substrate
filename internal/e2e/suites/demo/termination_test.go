// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package demo

import (
	"context"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGracefulWorkerTermination exercises the propagated-SIGTERM eviction flow
// end to end: an actor is scheduled onto a worker pod, that pod is deleted
// (simulating a Kubernetes eviction), and the control plane is expected to mark
// the worker DRAINING, then remove it and detach the actor once the pod is gone.
//
// The demo counter actor installs no SIGTERM handler, so it exits promptly and
// the actor lands in a terminal, non-RUNNING state (CRASHED unless it suspended
// cleanly first). We assert the control-plane state machine rather than any
// in-actor state saving, which is the application's responsibility.
func TestGracefulWorkerTermination(t *testing.T) {
	nsObj := e2e.CreateNamespace(t)

	ctx := context.Background()
	clients := e2e.GetClients()

	_, _ = clients.SubstrateAPI.CreateAtespace(ctx, &ateapipb.CreateAtespaceRequest{Name: demoAtespace})

	at, err := createActorTemplate(ctx, t, clients, nsObj, v1alpha1.SnapshotScopeFull, v1alpha1.SnapshotScopeFull)
	if err != nil {
		t.Fatalf("failed to initialize ActorTemplate: %v", err)
	}

	actorID := "graceful-term-" + nsObj.Name
	if _, err := clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{
		ActorRef:               &ateapipb.ActorRef{Atespace: demoAtespace, Name: actorID},
		ActorTemplateNamespace: nsObj.Name,
		ActorTemplateName:      at.Name,
	}); err != nil {
		t.Fatalf("failed to create Actor: %v", err)
	}
	defer func() {
		_, _ = clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{
			ActorRef: &ateapipb.ActorRef{Atespace: demoAtespace, Name: actorID},
		})
	}()

	// Bring the actor up on a worker so it is bound to a pod.
	if _, err := clients.SubstrateAPI.ResumeActor(ctx, &ateapipb.ResumeActorRequest{
		ActorRef: &ateapipb.ActorRef{Atespace: demoAtespace, Name: actorID},
	}); err != nil {
		t.Fatalf("failed to resume Actor: %v", err)
	}
	waitForActorStatus(ctx, t, clients, actorID, ateapipb.Actor_STATUS_RUNNING)

	running, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{
		ActorRef: &ateapipb.ActorRef{Atespace: demoAtespace, Name: actorID},
	})
	if err != nil {
		t.Fatalf("failed to get running Actor: %v", err)
	}
	podNS := running.GetActor().GetAteomPodNamespace()
	podName := running.GetActor().GetAteomPodName()
	if podNS == "" || podName == "" {
		t.Fatalf("running actor has no bound worker pod: ns=%q name=%q", podNS, podName)
	}
	t.Logf("Actor %q bound to worker pod %s/%s", actorID, podNS, podName)

	// Evict the worker pod. The kubelet sends SIGTERM to ateom, which propagates
	// it into the sandbox; the control plane marks the worker DRAINING on the
	// DeletionTimestamp watch event and cleans up when the pod is finally gone.
	if err := clients.K8s.CoreV1().Pods(podNS).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("failed to delete worker pod %s/%s: %v", podNS, podName, err)
	}

	// Best-effort: try to observe the transient DRAINING state. The demo actor
	// exits quickly, so the window can be short — log it but don't fail if missed.
	if drained := waitForWorkerDraining(ctx, t, clients, podName, 10*time.Second); drained {
		t.Logf("Observed worker %s in DRAINING state", podName)
	} else {
		t.Logf("Did not catch worker %s in DRAINING (likely drained too fast)", podName)
	}

	// The worker record must eventually be removed once the pod is gone.
	if err := waitForWorkerRemoved(ctx, t, clients, podName, 60*time.Second); err != nil {
		t.Fatalf("worker %s not removed after pod deletion: %v", podName, err)
	}

	// The actor must be detached from the dead pod and land in a terminal,
	// non-RUNNING state (CRASHED, or SUSPENDED if it suspended cleanly).
	deadline := time.Now().Add(60 * time.Second)
	for {
		got, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{
			ActorRef: &ateapipb.ActorRef{Atespace: demoAtespace, Name: actorID},
		})
		if err == nil {
			a := got.GetActor()
			st := a.GetStatus()
			if a.GetAteomPodName() == "" && st != ateapipb.Actor_STATUS_RUNNING && st != ateapipb.Actor_STATUS_RESUMING {
				if st != ateapipb.Actor_STATUS_CRASHED && st != ateapipb.Actor_STATUS_SUSPENDED {
					t.Fatalf("actor reached unexpected terminal status %v", st)
				}
				t.Logf("Actor %q detached from dead pod with status %v", actorID, st)
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("actor %q not detached from dead worker pod within timeout", actorID)
		}
		time.Sleep(time.Second)
	}
}

// waitForWorkerDraining polls ListWorkers until the named worker reports
// STATE_DRAINING or the timeout elapses. Returns true if DRAINING was observed.
func waitForWorkerDraining(ctx context.Context, t *testing.T, clients *e2e.Clients, podName string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := clients.SubstrateAPI.ListWorkers(ctx, &ateapipb.ListWorkersRequest{})
		if err == nil {
			for _, w := range resp.GetWorkers() {
				if w.GetWorkerPod() == podName && w.GetState() == ateapipb.Worker_STATE_DRAINING {
					return true
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// waitForWorkerRemoved polls ListWorkers until the named worker is absent.
func waitForWorkerRemoved(ctx context.Context, t *testing.T, clients *e2e.Clients, podName string, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		resp, err := clients.SubstrateAPI.ListWorkers(ctx, &ateapipb.ListWorkersRequest{})
		if err == nil {
			found := false
			for _, w := range resp.GetWorkers() {
				if w.GetWorkerPod() == podName {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		time.Sleep(time.Second)
	}
}
