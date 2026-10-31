package client

import (
	"testing"
	"time"
)

// TestDefaultClientHasTheDefaultTimeout checks the 30-second bound on a client
// built with no HTTP client (step 8 review S5). A server that stops answering
// would otherwise hang the caller, and the net/http default client has no timeout.
// It is an internal test, since the bound is a field of the client's own HTTP client.
func TestDefaultClientHasTheDefaultTimeout(t *testing.T) {
	c, err := New("http://127.0.0.1:8787", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.hc.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", c.hc.Timeout, DefaultTimeout)
	}
	if DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", DefaultTimeout)
	}
}
