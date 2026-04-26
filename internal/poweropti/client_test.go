package poweropti_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fedzzito/power-bridge/internal/config"
	"github.com/fedzzito/power-bridge/internal/poweropti"
)

// mockPoweropti starts a fake poweropti HTTP server that returns the given watts.
func mockPoweropti(t *testing.T, watts float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "testapikey" || pass != "testapikey" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"currentwatt": watts,
			"isvalid":     true,
			"obis1_8_0":   100.5,
			"obis2_8_0":   10.25,
		})
	}))
}

func TestClientPollsAndUpdatesReading(t *testing.T) {
	srv := mockPoweropti(t, 1500.0)
	defer srv.Close()

	cfg := config.Defaults()
	cfg.PoweroptiIP = srv.Listener.Addr().String()
	cfg.PoweroptiAPIKey = "testapikey"
	cfg.PollIntervalS = 1
	cfg.StaleTimeoutS = 5

	client := poweropti.NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.Run(ctx)

	// Wait for at least one successful poll.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rd := client.Latest()
		if rd.Valid && rd.Watt == 1500.0 {
			// Also check energy counters (100.5 kWh → 100500 Wh)
			if rd.ConsumedWh != 100500.0 {
				t.Errorf("ConsumedWh: want 100500, got %v", rd.ConsumedWh)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("no valid reading received within timeout; last: %+v", client.Latest())
}

func TestClientFeedInNegative(t *testing.T) {
	srv := mockPoweropti(t, -800.0) // negative = feeding into grid
	defer srv.Close()

	cfg := config.Defaults()
	cfg.PoweroptiIP = srv.Listener.Addr().String()
	cfg.PoweroptiAPIKey = "testapikey"
	cfg.PollIntervalS = 1
	cfg.StaleTimeoutS = 5

	client := poweropti.NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rd := client.Latest()
		if rd.Valid && rd.Watt == -800.0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("expected watt=-800, got %+v", client.Latest())
}

func TestClientLatestThreadSafe(t *testing.T) {
	cfg := config.Defaults()
	cfg.PoweroptiIP = "127.0.0.1:1"
	cfg.PoweroptiAPIKey = "key"
	cfg.PollIntervalS = 60
	cfg.StaleTimeoutS = 10

	client := poweropti.NewClient(cfg)

	// Concurrent reads should not race
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_ = client.Latest()
			done <- struct{}{}
		}()
	}
	timeout := time.After(2 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("timed out waiting for goroutines")
		}
	}
}

func TestClientFreshHasNoValidReading(t *testing.T) {
	cfg := config.Defaults()
	client := poweropti.NewClient(cfg)
	rd := client.Latest()
	if rd.Valid {
		t.Error("fresh client should not have valid reading")
	}
	if !rd.At.IsZero() {
		t.Error("fresh client At should be zero time")
	}
}

