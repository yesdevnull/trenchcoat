package server_test

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

func TestServe_Reload(t *testing.T) {
	initialCoats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "v1",
				Request:  coat.Request{Method: "GET", URI: "/test"},
				Response: &coat.Response{Code: 200, Body: "version-1"},
			},
		},
	}

	srv := server.New(initialCoats, server.Config{})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Initial request should get v1 response.
	resp, err := http.Get(srv.URL() + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if string(body) != "version-1" {
		t.Fatalf("expected version-1, got %s", body)
	}

	// Reload with new coats.
	newCoats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "v2",
				Request:  coat.Request{Method: "GET", URI: "/test"},
				Response: &coat.Response{Code: 200, Body: "version-2"},
			},
		},
	}
	srv.Reload(newCoats)

	// Request should now get v2 response.
	resp2, err := http.Get(srv.URL() + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()

	if string(body2) != "version-2" {
		t.Fatalf("expected version-2, got %s", body2)
	}
}

func TestServe_Reload_ResetsSequenceCounters(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:    "seq",
				Request: coat.Request{Method: "GET", URI: "/seq"},
				Responses: []coat.Response{
					{Code: 200, Body: "first"},
					{Code: 200, Body: "second"},
				},
				Sequence: "once",
			},
		},
	}

	srv := server.New(coats, server.Config{})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Exhaust the sequence.
	resp, _ := http.Get(srv.URL() + "/seq")
	_ = resp.Body.Close()
	resp, _ = http.Get(srv.URL() + "/seq")
	_ = resp.Body.Close()
	resp, _ = http.Get(srv.URL() + "/seq")
	_ = resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 after exhaustion, got %d", resp.StatusCode)
	}

	// Reload with same coats — counters should reset.
	srv.Reload(coats)

	resp2, _ := http.Get(srv.URL() + "/seq")
	body, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if string(body) != "first" {
		t.Fatalf("expected 'first' after reload, got %s", body)
	}
}

func TestServe_Reload_ConcurrentRequests(t *testing.T) {
	coatsV1 := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "v1",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, Body: "v1"},
			},
		},
	}
	coatsV2 := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "v2",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, Body: "v2"},
			},
		},
	}

	srv := server.New(coatsV1, server.Config{})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Fire concurrent requests while toggling reloads.
	// Each response must be coherent: either "v1" or "v2", never empty or mixed.
	const numRequests = 100
	const numReloads = 20

	var wg sync.WaitGroup

	// Goroutine that toggles reloads.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range numReloads {
			if i%2 == 0 {
				srv.Reload(coatsV2)
			} else {
				srv.Reload(coatsV1)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutines that send requests.
	errs := make(chan string, numRequests)
	for range numRequests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(srv.URL() + "/data")
			if err != nil {
				errs <- "request error: " + err.Error()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			b := string(body)
			if b != "v1" && b != "v2" {
				errs <- "unexpected body: " + b
			}
		}()
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}
