//go:build linux

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

package main

import (
	"context"
	"slices"
	"testing"

	"github.com/agent-substrate/substrate/internal/proto/ateompb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func drainingService() *AteomService {
	return &AteomService{shuttingDown: true, running: map[string]*runningActor{}}
}

func TestRunWorkload_RejectsWhenDraining(t *testing.T) {
	_, err := drainingService().RunWorkload(context.Background(), &ateompb.RunWorkloadRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("RunWorkload while draining = %v, want codes.Unavailable", err)
	}
}

func TestRestoreWorkload_RejectsWhenDraining(t *testing.T) {
	_, err := drainingService().RestoreWorkload(context.Background(), &ateompb.RestoreWorkloadRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("RestoreWorkload while draining = %v, want codes.Unavailable", err)
	}
}

func TestCheckpointWorkload_RejectsWhenDraining(t *testing.T) {
	_, err := drainingService().CheckpointWorkload(context.Background(), &ateompb.CheckpointWorkloadRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("CheckpointWorkload while draining = %v, want codes.Unavailable", err)
	}
}

// TestGracefulShutdown_NoActors verifies the shutdown path flips the draining
// flag and returns cleanly when no actor is running (the common idle-worker case).
func TestGracefulShutdown_NoActors(t *testing.T) {
	s := &AteomService{running: map[string]*runningActor{}}
	s.gracefulShutdown(context.Background())
	if !s.shuttingDown {
		t.Errorf("expected shuttingDown=true after gracefulShutdown")
	}
}

func TestOverlayWorkloadIDs(t *testing.T) {
	got := overlayWorkloadIDs([]actorContainer{{name: "main"}, {name: "sidecar"}})
	want := []string{"main_ovl", "sidecar_ovl"}
	if !slices.Equal(got, want) {
		t.Errorf("overlayWorkloadIDs = %v, want %v", got, want)
	}
}
