// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"context"
	"time"
)

// ChaosTimer configures a chaos schedule.
type ChaosTimer interface {
	Timing() (time.Duration, time.Duration)
}

// Periodic is a chaos timing using fixed durations.
type Periodic struct {
	Down, Up time.Duration
}

// Timing implements ChaosTimer.
func (p Periodic) Timing() (time.Duration, time.Duration) {
	return p.Down, p.Up
}

// Chaos stops and restarts nodes in a cluster.
type Chaos struct {
	// Timing is consulted before each chaos event. It provides the duration of
	// the downtime and the subsequent chaos-free duration.
	Timer ChaosTimer
	// Target is consulted before each chaos event to determine the node(s) which
	// should be killed.
	Target func() nodeListOption
	// Stopper is a channel that the chaos agent listens on. The agent will
	// terminate cleanly once it receives on the channel.
	Stopper <-chan time.Time
	// DrainAndQuit is used to determine if want to kill the node vs draining it
	// first and shutting down gracefully.
	DrainAndQuit bool
}

// Runner returns a closure that runs chaos against the given cluster without
// setting off the monitor. The process returns without an error after the chaos
// duration.
func (ch *Chaos) Runner(c *cluster, m *monitor) func(context.Context) error {
	return func(ctx context.Context) error {
		l, err := c.l.childLogger("CHAOS")
		if err != nil {
			return err
		}
		for {
			before, between := ch.Timer.Timing()
			select {
			case <-ch.Stopper:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(before):
			}

			target := ch.Target()
			m.ExpectDeath()

			if ch.DrainAndQuit {
				l.printf("stopping and draining %v (slept %s)\n", target, before)
				c.Stop(ctx, target, stopArgs("--sig=15"))
			} else {
				l.printf("killing %v (slept %s)\n", target, before)
				c.Stop(ctx, target)
			}

			select {
			case <-ch.Stopper:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(between):
			}

			c.l.printf("restarting %v after %s of downtime\n", target, between)
			c.Start(ctx, target)
		}
	}
}
