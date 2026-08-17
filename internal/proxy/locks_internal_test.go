package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProxy_ReleasesPerCaptureStateWhenCapturesFinish checks that the state a
// capture keys by base name is gone once the capture has finished.
//
// Nothing about the captured files shows this: the entries are dead the moment
// the capture completes, so a proxy that keeps them forever produces exactly the
// same coat files. Only the maps themselves say whether they were reclaimed. A
// proxy pointed at per-resource paths sees a distinct base name per resource,
// and a leak there is one map entry plus one mutex per URL for the life of the
// process, reclaimable only by restarting.
//
// It is an internal test because the maps are the subject: an assertion made
// through the public API could not tell a reaped map from a retained one.
func TestProxy_ReleasesPerCaptureStateWhenCapturesFinish(t *testing.T) {
	const requests = 20

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	p, err := New(Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     t.TempDir(),
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
	t.Cleanup(func() { _ = p.Shutdown(10 * time.Second) })

	client := &http.Client{Timeout: 30 * time.Second}
	for i := range requests {
		resp, err := client.Get(fmt.Sprintf("%s/users/%d", p.URL(), i))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	p.WaitCaptures()

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.baseLocks) != 0 {
		t.Errorf("baseLocks still holds %d entries after every capture finished; they are never reclaimed", len(p.baseLocks))
	}
	if len(p.inflight) != 0 {
		t.Errorf("inflight still holds %d entries after every capture finished", len(p.inflight))
	}
}
