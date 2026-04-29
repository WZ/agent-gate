package dashboard

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveStreamEmitsNewEventIDsAsTheyAppear(t *testing.T) {
	opts := freshOpts(t)
	opts.LivePollInterval = 50 * time.Millisecond
	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/live", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{ID: "live-1", StartedAt: time.Now()},
		}})
	}()

	scanner := bufio.NewScanner(resp.Body)
	got := ""
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") && strings.Contains(line, "live-1") {
			got = line
			break
		}
	}
	assert.Contains(t, got, "live-1")

	_, _ = io.Copy(io.Discard, resp.Body)
}
