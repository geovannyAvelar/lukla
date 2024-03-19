package internal

import (
	"os"
	"testing"
	"time"
)

func TestGetHttpClientTimeout(t *testing.T) {
	t.Parallel()

	os.Setenv("LUKLA_HTTP_CLIENT_TIMEOUT", "10")

	timeout := GetHttpClientTimeout()
	expected := 10 * time.Second

	if timeout != expected {
		t.Errorf("Expected %s but received %s", timeout, expected)
	}
}
