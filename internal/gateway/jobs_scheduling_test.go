package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestDDNSGroupsRunWithBoundedConcurrencyAndCoalescedKicks(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal("fixture setup failed")
	}
	for i := 0; i < 6; i++ {
		group := testDDNSGroup()
		group.ID, group.Name, group.Enabled = fmt.Sprint(i), fmt.Sprint(i), true
		store.state.Config.DDNS.Groups = append(store.state.Config.DDNS.Groups, group)
	}
	jobs := NewJobs(store, nil)
	for i := 0; i < 1000; i++ {
		if !jobs.TriggerDDNS("") {
			t.Fatal("coalesced trigger was rejected")
		}
	}
	if len(jobs.ddnsKick) != 1 {
		t.Fatal("same trigger occupied multiple queue slots")
	}
	ctx, cancel := context.WithCancel(context.Background())
	started, release := make(chan string, 6), make(chan struct{})
	var active atomic.Int32
	jobs.provider = func(_ State, group DDNSGroup, _ *Logs) (dnsProvider, error) {
		active.Add(1)
		defer active.Add(-1)
		started <- group.ID
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, errors.New("TEST_ONLY_PROVIDER_STOP")
	}
	done := make(chan struct{})
	go func() { jobs.ddnsLoop(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("a slow group blocked other groups")
		}
	}
	if active.Load() != 4 {
		t.Fatal("DDNS concurrency exceeded its limit")
	}
	select {
	case <-started:
		t.Fatal("fifth task started before a slot was released")
	default:
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("waiting groups did not resume")
		}
	}
}

func TestCertificateFailuresBackOffWithoutManualBypass(t *testing.T) {
	now := time.Now()
	var retry certificateRetry
	for _, delay := range []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour} {
		retry.failed(now)
		if retry.next.Sub(now) != delay || !now.Add(delay-time.Second).Before(retry.next) {
			t.Fatal("certificate cooldown did not increase and cap at one hour")
		}
		now = retry.next
	}
}
