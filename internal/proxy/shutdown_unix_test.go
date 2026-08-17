//go:build unix

package proxy_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/proxy"
)

// TestProxy_ShutdownWaitsForCaptureFromInFlightRequest pins the ordering of the
// two drains in Shutdown.
//
// Waiting on the capture group before stopping the HTTP server loses the
// captures belonging to requests that were still in flight: the wait finds an
// empty group and returns at once, and the handlers that follow register their
// captures with nothing left to wait for them. Pressing Ctrl-C mid-request then
// silently discards exactly the coat files the user was recording.
//
// Timing cannot decide this. The window is however long the outstanding capture
// work happens to take against however long http.Server.Shutdown happens to
// take to notice idle connections, and tuning payload sizes against that only
// produces a test that catches the bug some of the time. Instead the capture is
// blocked outright: a FIFO sits at the path the capture will write to, and
// os.WriteFile blocks there until something opens the read end. The correct
// ordering must therefore burn its whole capture-drain budget, while the broken
// ordering returns as soon as the connections are idle. That is a difference of
// a second, not of microseconds, and it does not depend on load.
//
// Unix-only: it needs mkfifo. CI runs the suite on ubuntu-latest.
func TestProxy_ShutdownWaitsForCaptureFromInFlightRequest(t *testing.T) {
	const (
		shutdownTimeout = 2 * time.Second
		captureDrain    = shutdownTimeout / 2
	)

	release := make(chan struct{})
	var arrived sync.WaitGroup
	arrived.Add(1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived.Done()
		<-release
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("captured"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()

	// With dedupe=overwrite the capture path is fully determined: METHOD,
	// sanitised URL path, status. The FIFO goes where the capture actually
	// writes -- the temp file it renames from -- so the write blocks there.
	coatPath := blockingPathFor(writeDir, "GET_blocked_200.yaml")
	if err := syscall.Mkfifo(coatPath, 0600); err != nil {
		t.Fatalf("failed to create fifo at %s: %v", coatPath, err)
	}

	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		// The correct ordering logs a capture-drain timeout here by design;
		// discard it rather than let it dirty the test output.
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}

	// Unblock the capture goroutine at the end, whatever happens, so it does not
	// outlive the test parked in a write.
	t.Cleanup(func() {
		fifo, err := os.OpenFile(coatPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, fifo)
		_ = fifo.Close()
	})

	client := &http.Client{Timeout: 30 * time.Second}
	var request sync.WaitGroup
	request.Add(1)
	go func() {
		defer request.Done()
		resp, err := client.Get(p.URL() + "/blocked")
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// The request is parked in the upstream, so no capture has registered yet:
	// this is the state Shutdown finds on Ctrl-C mid-request.
	waitOrFail(t, &arrived, 30*time.Second, "the request to reach the upstream")

	shutdownErr := make(chan error, 1)
	started := time.Now()
	go func() { shutdownErr <- p.Shutdown(shutdownTimeout) }()
	time.Sleep(10 * time.Millisecond)
	close(release)

	if err := <-shutdownErr; err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	elapsed := time.Since(started)
	waitOrFail(t, &request, 30*time.Second, "the client request to finish")

	// Blocked on the FIFO, the capture can never finish, so a Shutdown that
	// waits for it must spend its whole capture-drain budget. Returning early
	// means it never waited, and a real capture would have been lost.
	if elapsed < captureDrain {
		t.Fatalf("Shutdown returned after %s without waiting for the capture registered by the in-flight request; expected it to wait out the %s capture drain",
			elapsed.Round(time.Millisecond), captureDrain)
	}
}

// TestProxy_ShutdownDoesNotDrainCapturesAfterAFailedHTTPDrain covers the case
// the ordering fix left open.
//
// http.Server.Shutdown returns ctx.Err() when its deadline passes with
// connections still active, and it does not stop the handlers running on them.
// Waiting on the capture group at that point is the very race the ordering fix
// was meant to close: a handler still in flight can call captures.Go while
// Wait is in progress, and an Add that takes the counter from zero to positive
// during a Wait is documented WaitGroup misuse that panics when the
// interleaving lands.
//
// The panic itself needs an interleaving too narrow to force reliably, so this
// pins the observable half. With a capture parked on a FIFO, a Shutdown that
// goes on to wait spends its whole capture-drain budget; one that correctly
// declines to wait returns as soon as the HTTP drain gives up.
func TestProxy_ShutdownDoesNotDrainCapturesAfterAFailedHTTPDrain(t *testing.T) {
	const (
		shutdownTimeout = 2 * time.Second
		httpDrain       = shutdownTimeout / 2
	)

	parked := make(chan struct{})
	var parkedArrived sync.WaitGroup
	parkedArrived.Add(1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/parked" {
			parkedArrived.Done()
			<-parked
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("captured"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	coatPath := blockingPathFor(writeDir, "GET_blocked_200.yaml")
	if err := syscall.Mkfifo(coatPath, 0600); err != nil {
		t.Fatalf("failed to create fifo at %s: %v", coatPath, err)
	}

	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// One capture that can never finish, so the capture group stays non-empty.
	resp, err := client.Get(p.URL() + "/blocked")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// One request that never returns, so the HTTP drain must time out.
	var parkedRequest sync.WaitGroup
	parkedRequest.Add(1)
	go func() {
		defer parkedRequest.Done()
		resp, err := client.Get(p.URL() + "/parked")
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	waitOrFail(t, &parkedArrived, 30*time.Second, "the parked request to reach the upstream")

	// Let the blocked capture reach its write before shutting down.
	time.Sleep(100 * time.Millisecond)

	started := time.Now()
	shutdownErr := p.Shutdown(shutdownTimeout)
	elapsed := time.Since(started)

	// Release everything before asserting, so a failure cannot hang the suite.
	close(parked)
	drainFifo(t, coatPath, 30*time.Second)
	waitOrFail(t, &parkedRequest, 30*time.Second, "the parked request to finish")

	// The capture this test deliberately abandoned is now unblocked and about to
	// rename its temp file into the write directory. Let it finish before the
	// test returns, or t.TempDir's cleanup races the rename and fails with
	// "directory not empty".
	captures := make(chan struct{})
	go func() {
		p.WaitCaptures()
		close(captures)
	}()
	select {
	case <-captures:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the released capture to finish")
	}

	if shutdownErr == nil {
		t.Fatal("expected Shutdown to report the HTTP drain deadline")
	}
	if elapsed > httpDrain+500*time.Millisecond {
		t.Fatalf("Shutdown took %s: after the HTTP drain failed it went on to wait for captures, which races captures.Go in the handlers still running",
			elapsed.Round(time.Millisecond))
	}
}

// waitOrFail waits for wg, failing instead of blocking the whole package if the
// requests never arrive. The request goroutines swallow their errors, so a
// connection refused or an ephemeral-port exhaustion would otherwise leave the
// counter short and hang until the 10-minute package timeout, taking every
// other test's result with it.
func waitOrFail(t *testing.T, wg *sync.WaitGroup, within time.Duration, what string) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("timed out after %s waiting for %s", within, what)
	}
}

// blockingPathFor returns the path a capture for coatName writes before
// renaming it into place. Placing a FIFO there parks the capture in its write.
//
// Captures write to a temp file and rename, so a FIFO at the destination is
// never opened and cannot block anything -- the block has to sit where the
// bytes actually go. The name mirrors proxy.tempPathFor.
func blockingPathFor(dir, coatName string) string {
	return filepath.Join(dir, "."+coatName+".tmp")
}
