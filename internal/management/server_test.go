package management

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"stp/internal/logging"
)

func TestServerMetrics(t *testing.T) {
	logger := logging.New(logging.LevelError, io.Discard)
	srv, err := New(
		"127.0.0.1:0",
		func() interface{} { return map[string]int{"value": 1} },
		logger,
		WithMetrics(func() map[string]float64 {
			return map[string]float64{"stp_test_metric": 42}
		}),
		WithACL([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Start()
	defer srv.Close(context.Background())

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + srv.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "stp_test_metric") {
		t.Fatalf("metrics output missing expected metric: %s", body)
	}
}

func TestServerACL(t *testing.T) {
	s := &Server{}
	allowPrefixes := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	s.SetACL(allowPrefixes)

	if !s.allowed("127.0.0.1:1234") {
		t.Fatalf("expected request from loopback to be allowed")
	}
	if s.allowed("203.0.113.1:8080") {
		t.Fatalf("expected request outside ACL to be rejected")
	}
}
