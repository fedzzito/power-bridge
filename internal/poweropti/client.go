// Package poweropti implements a polling client for the powerfox poweropti
// local REST API.
//
// The poweropti exposes its readings at:
//
//	GET http://<ip>/api/user/current
//
// Authentication uses HTTP Basic Auth with the API key as both username and
// password (as documented by powerfox for the local endpoint).
//
// Response (JSON):
//
//	{
//	  "currentwatt": 1234.5,   // current power in W  (>0 = consume, <0 = feed)
//	  "isvalid": true,
//	  "obis1_8_0": 12345.678,  // total consumed energy in kWh
//	  "obis2_8_0":   100.000   // total delivered energy in kWh
//	}
//
// If the device returns a different schema the MWatt field (milliwatts) is
// also supported as fallback.
package poweropti

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/fedzzito/power-bridge/internal/config"
)

// Reading is one snapshot of poweropti measurements.
type Reading struct {
	// Watt is the current net power in watts.
	// Positive  → consuming from grid.
	// Negative  → feeding into grid.
	Watt float64

	// ConsumedWh is the total energy consumed from the grid (Wh).
	ConsumedWh float64

	// DeliveredWh is the total energy fed into the grid (Wh).
	DeliveredWh float64

	// Valid indicates whether the latest poll succeeded.
	Valid bool

	// Timestamp of the last successful poll.
	At time.Time
}

// apiResponse maps the JSON fields returned by the poweropti local API.
type apiResponse struct {
	CurrentWatt float64 `json:"currentwatt"`
	MWatt       float64 `json:"mw"`       // milliwatts – alternate format
	IsValid     *bool   `json:"isvalid"`  // pointer distinguishes absent from false
	Obis180     float64 `json:"obis1_8_0"` // kWh consumed
	Obis280     float64 `json:"obis2_8_0"` // kWh delivered
	WhIn        float64 `json:"wh_in"`     // alternate: Wh consumed
	WhOut       float64 `json:"wh_out"`    // alternate: Wh delivered
}

// Client polls the poweropti and exposes the latest Reading.
type Client struct {
	cfg    *config.Config
	mu     sync.RWMutex
	latest Reading
	errors int // consecutive error count
}

// NewClient creates a Client from the given config.
func NewClient(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

// Run starts the polling loop and blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	interval := time.Duration(c.cfg.PollIntervalS) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Poll immediately on start.
	c.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

// Latest returns the most recent Reading in a thread-safe manner.
func (c *Client) Latest() Reading {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// ConsecutiveErrors returns how many polls have failed in a row.
func (c *Client) ConsecutiveErrors() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.errors
}

func (c *Client) poll(ctx context.Context) {
	r, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.errors++
		stale := time.Duration(c.cfg.StaleTimeoutS) * time.Second
		if time.Since(c.latest.At) > stale {
			c.latest.Valid = false
		}
		log.Printf("poweropti poll error (#%d): %v", c.errors, err)
		return
	}
	c.errors = 0
	c.latest = *r
}

func (c *Client) fetch(ctx context.Context) (*Reading, error) {
	url := fmt.Sprintf("http://%s/api/user/current", c.cfg.PoweroptiIP)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.PoweroptiAPIKey, c.cfg.PoweroptiAPIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var ar apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&ar); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Determine validity.
	// When the API includes isvalid, trust it explicitly.
	// When isvalid is absent (older firmware), fall back to non-zero power.
	var valid bool
	switch {
	case ar.IsValid != nil:
		valid = *ar.IsValid
	default:
		valid = ar.CurrentWatt != 0 || ar.MWatt != 0
	}
	reading := &Reading{
		Valid: valid,
		At:    time.Now(),
	}

	// Prefer currentwatt; fall back to mw (milliwatts).
	switch {
	case ar.CurrentWatt != 0:
		reading.Watt = ar.CurrentWatt
	case ar.MWatt != 0:
		reading.Watt = ar.MWatt / 1000.0
	}

	// Energy counters: prefer OBIS codes (kWh → Wh), fall back to wh_in/wh_out.
	if ar.Obis180 != 0 {
		reading.ConsumedWh = ar.Obis180 * 1000
		reading.DeliveredWh = ar.Obis280 * 1000
	} else {
		reading.ConsumedWh = ar.WhIn
		reading.DeliveredWh = ar.WhOut
	}

	return reading, nil
}
