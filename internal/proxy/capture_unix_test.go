//go:build unix

package proxy_test

import (
	"fmt"
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

// TestProxy_CaptureConcurrencyIsBounded checks that captureSem actually bounds
// concurrent capture writes.
//
// Counting the files a burst of requests produces cannot show this: the bound
// is invisible once the captures have finished, so such a test passes with the
// semaphore deleted. Blocking makes it visible. FIFOs are placed at the paths
// the first 20 captures will write to, so those captures park in os.WriteFile
// holding every semaphore slot. The remaining captures cannot start, and their
// files must be absent for as long as the slots stay occupied. Free the slots
// and they appear.
//
// Unix-only: it needs mkfifo. CI runs the suite on ubuntu-latest.
func TestProxy_CaptureConcurrencyIsBounded(t *testing.T) {
	const (
		bound  = proxy.CaptureConcurrency
		extra  = 5 // requests beyond the bound
		total  = bound + extra
		settle = 250 * time.Millisecond
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	writeDir := t.TempDir()

	// The first `bound` captures block; with dedupe=overwrite each path maps to
	// exactly one filename, so the temp file each one writes can be picked in
	// advance and a FIFO placed there.
	blockedPaths := make([]string, 0, bound)
	for i := range bound {
		p := blockingPathFor(writeDir, fmt.Sprintf("GET_b_%d_200.yaml", i))
		if err := syscall.Mkfifo(p, 0600); err != nil {
			t.Fatalf("failed to create fifo %s: %v", p, err)
		}
		blockedPaths = append(blockedPaths, p)
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
	addr, err := p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}

	// Cleanups run last-registered-first, so this pair drains the FIFOs before
	// shutting the proxy down -- otherwise Shutdown would sit out its whole
	// capture drain waiting for captures that nothing has released.
	t.Cleanup(func() { _ = p.Shutdown(10 * time.Second) })

	drained := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-drained:
			return
		default:
		}
		// Best effort: O_NONBLOCK so this cannot hang if no capture is waiting.
		for _, path := range blockedPaths {
			fifo, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, fifo)
			_ = fifo.Close()
		}
	})

	client := &http.Client{Timeout: 30 * time.Second}
	request := func(from, to int) {
		var wg sync.WaitGroup
		for i := from; i < to; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				resp, err := client.Get(fmt.Sprintf("http://%s/b/%d", addr, n))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}(i)
		}
		waitOrFail(t, &wg, 60*time.Second, "the request batch to finish")
	}

	// The blocking captures must be the ones holding the slots, so they go
	// first and are given time to reach their writes. Firing all the requests
	// at once would leave it to the scheduler which captures take the slots,
	// and a capture still queued for a slot is not parked in its FIFO -- which
	// both weakens the assertion below and hangs the release loop, since it
	// waits for a writer that has not arrived.
	request(0, bound)
	time.Sleep(settle)

	// Every slot is now held by a capture blocked on its FIFO, so captures
	// beyond the bound cannot run.
	request(bound, total)
	time.Sleep(settle)
	for i := bound; i < total; i++ {
		path := filepath.Join(writeDir, fmt.Sprintf("GET_b_%d_200.yaml", i))
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("%s was written while all %d capture slots were blocked; captures are not bounded",
				filepath.Base(path), bound)
		}
	}

	// Releasing the blocked captures must let the rest through.
	//
	// A blocking open is required here. Each capture is parked in open-for-write
	// waiting for a reader, and this open completes that rendezvous. Opening
	// with O_NONBLOCK instead returns a descriptor that reads EOF straight away
	// -- a FIFO with no writer attached yet -- so closing it would leave the
	// capture still waiting for a reader that has already gone.
	for _, path := range blockedPaths {
		fifo, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("failed to open fifo %s for reading: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, fifo)
		_ = fifo.Close()
	}
	close(drained)

	p.WaitCaptures()

	for i := bound; i < total; i++ {
		path := filepath.Join(writeDir, fmt.Sprintf("GET_b_%d_200.yaml", i))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s never appeared after the blocked captures were released: %v", filepath.Base(path), err)
		}
	}
}
