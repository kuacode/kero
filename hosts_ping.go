package main

import (
	"errors"
	"sync"
	"time"

	"kero/internal/remote"
)

// errPingTimeout and errPingClosed say why a round trip did not come: the
// host took longer than the limit, or the connection ended under it.
var (
	errPingTimeout = errors.New("ping timed out")
	errPingClosed  = errors.New("connection closed")
)

// The app reads a remote host's round trip the way terminal clients that
// survive a network (mosh, Eternal Terminal) do: a small heartbeat of its
// own, a moving average of it, not the last sample.  What it shows is the
// smoothed round trip (SRTT), as RFC 6298 has TCP keep it, with the
// variance (RTTVAR) beside it as the jitter: one sample that lands slow,
// as a scheduler hiccup, leaves a mark, not a new figure.
//
// The cadence follows the reading: a beat while a host is new or drags,
// a slack one once it reads steadily, so a quiet connection costs almost
// nothing and a bad one is followed closely.
const (
	pingWarm     = 4                      // samples before settling to a slack cadence
	pingFast     = time.Second            // while warming up, or just degraded
	pingDegraded = 2 * time.Second        // while a host drags or stopped answering
	pingSteady   = 5 * time.Second        // once it reads steadily
	pingTimeout  = 4 * time.Second        // the longest a single round trip may take
	rttSlow      = 150 * time.Millisecond // the SRTT past which a host reads as dragging
	rttFar       = 400 * time.Millisecond // the SRTT past which it is far
)

// watchPing keeps the round trip to each remote host as the sidebar's
// header shows it: it beats on every ready host, folds what comes into
// the host's moving average, and lengthens or shortens the pause as the
// hosts read.
func (a *App) watchPing() {
	for {
		type target struct {
			h  *host
			rc *remote.Conn
		}
		var (
			targets          []target
			warming, degrade bool
		)
		a.update(func() {
			for _, h := range a.hosts {
				if !h.remote() || h.rc == nil || h.conn != connReady {
					continue
				}
				targets = append(targets, target{h, h.rc})
				if h.rttSamples < pingWarm {
					warming = true
				}
				if h.rttLost || (h.rttSamples > 0 && h.rtt >= rttSlow) {
					degrade = true
				}
			}
		})
		if len(targets) == 0 {
			time.Sleep(pingSteady)
			continue
		}
		// Beat on every host at once, so that one far away, or gone,
		// holds up no other; gather what came, and take it in one update.
		type result struct {
			t    target
			d    time.Duration
			lost bool
		}
		results := make([]result, len(targets))
		var wg sync.WaitGroup
		for i, t := range targets {
			wg.Add(1)
			go func(i int, t target) {
				defer wg.Done()
				d, err := pingOnce(t.rc, pingTimeout)
				results[i] = result{t: t, d: d, lost: err != nil}
			}(i, t)
		}
		wg.Wait()
		a.update(func() {
			for _, r := range results {
				if r.t.h.rc != r.t.rc || r.t.h.conn != connReady {
					continue
				}
				if r.lost {
					// A host that stopped answering shows no figure
					// rather than the last one it gave.
					r.t.h.forgetRTT()
					r.t.h.rttLost = true
					continue
				}
				r.t.h.observeRTT(r.d)
				r.t.h.rttLost = false
			}
		})
		switch {
		case warming:
			time.Sleep(pingFast)
		case degrade:
			time.Sleep(pingDegraded)
		default:
			time.Sleep(pingSteady)
		}
	}
}

// pingOnce measures the round trip to the host, or gives up after limit
// and says so, so a host that hangs holds up no other.
func pingOnce(rc *remote.Conn, limit time.Duration) (time.Duration, error) {
	type result struct {
		d   time.Duration
		err error
	}
	ch := make(chan result, 1)
	go func() {
		d, err := rc.Ping()
		ch <- result{d, err}
	}()
	select {
	case r := <-ch:
		return r.d, r.err
	case <-time.After(limit):
		return 0, errPingTimeout
	case <-rc.Done():
		return 0, errPingClosed
	}
}
