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
	"slices"
	"testing"
)

func TestRunscKillArgs(t *testing.T) {
	r := &runsc{path: "/bin/runsc", atespace: "team", actorID: "actor1"}
	args := r.killArgs("main", "SIGTERM")

	// The subcommand + operands must appear in order at the tail: `kill <name> <signal>`.
	want := []string{"kill", "main", "SIGTERM"}
	if len(args) < len(want) || !slices.Equal(args[len(args)-len(want):], want) {
		t.Errorf("killArgs = %v, want suffix %v", args, want)
	}
	if !slices.Contains(args, "-root") {
		t.Errorf("killArgs missing -root scoping: %v", args)
	}
}

func TestRunscWaitArgs(t *testing.T) {
	r := &runsc{path: "/bin/runsc", atespace: "team", actorID: "actor1"}
	args := r.waitArgs("main")

	want := []string{"wait", "main"}
	if len(args) < len(want) || !slices.Equal(args[len(args)-len(want):], want) {
		t.Errorf("waitArgs = %v, want suffix %v", args, want)
	}
	if !slices.Contains(args, "-root") {
		t.Errorf("waitArgs missing -root scoping: %v", args)
	}
}
