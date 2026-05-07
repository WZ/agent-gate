package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-gate/internal/ca"
	"agent-gate/internal/idgen"
	"agent-gate/internal/types"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHijackForwardsUpgradeAndPersistsMessages exercises the full WebSocket
// hijack path: the proxy takes ownership of the CONNECT, terminates TLS with
// our local CA, dials the upstream, forwards the upgrade, and runs frame
// pumps. We verify that:
//
//   - The upgrade is recorded as a parent flow (kind: HTTP 101).
//   - A client→server text message is recorded with parent_id + direction "c2s".
//   - A server→client text message is recorded with parent_id + direction "s2c".
//   - Frame payloads round-trip through both pumps unchanged.
func TestHijackForwardsUpgradeAndPersistsMessages(t *testing.T) {
	upstream := newEchoWSServer(t)
	defer upstream.Close()

	upstreamHost := upstream.Listener.Addr().String() // 127.0.0.1:PORT
	upstreamServerName, _, err := net.SplitHostPort(upstreamHost)
	require.NoError(t, err)

	dir := t.TempDir()
	testCA, err := ca.Ensure(dir)
	require.NoError(t, err)

	upstreamRoots := x509.NewCertPool()
	upstreamRoots.AddCert(upstream.Certificate())

	out := make(chan types.RawFlow, 16)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	opts := Options{
		Listener:        listener,
		CA:              testCA,
		Out:             out,
		IDGen:           idgen.NewGenerator(),
		CaptureMode:     "permissive",
		UpstreamRootCAs: upstreamRoots,
		BodyLimit:       1 << 20,
		HijackHost: func(h string) bool {
			return h == upstreamServerName
		},
	}

	srvErr := make(chan error, 1)
	go func() { srvErr <- Run(opts) }()
	defer listener.Close()

	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(testCA.Cert)

	rawConn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	require.NoError(t, err)
	defer rawConn.Close()

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", upstreamHost, upstreamHost)
	_, err = rawConn.Write([]byte(connectReq))
	require.NoError(t, err)

	br := bufio.NewReader(rawConn)
	statusLine, err := br.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.Contains(statusLine, "200"), "expected 200 from proxy CONNECT, got %q", statusLine)
	for {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	tlsClient := tls.Client(rawConn, &tls.Config{
		ServerName: upstreamServerName,
		RootCAs:    clientCAs,
	})
	require.NoError(t, tlsClient.Handshake())

	// coder/websocket's Dial doesn't expose a way to use a pre-established
	// net.Conn directly through its public API, so we hand-roll the WS upgrade
	// using a tiny custom Transport that returns the existing TLS conn.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := (&url.URL{Scheme: "https", Host: upstreamHost, Path: "/ws"}).String()
	wsConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &fixedConnTransport{conn: tlsClient, host: upstreamHost},
		},
	})
	require.NoError(t, err)
	defer wsConn.Close(websocket.StatusNormalClosure, "test done")

	require.NoError(t, wsConn.Write(ctx, websocket.MessageText, []byte("ping from client")))

	msgType, payload, err := wsConn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, msgType)
	assert.Equal(t, []byte("echo: ping from client"), payload)

	// Close the WS cleanly so the s2c pump terminates and emits its events.
	wsConn.Close(websocket.StatusNormalClosure, "")

	flows := drainFlows(t, out, 3, 3*time.Second)

	parent, c2s, s2c := classifyHijackFlows(t, flows)

	// Parent upgrade event: 101 Switching Protocols, GET /ws, scheme wss.
	assert.Equal(t, http.StatusSwitchingProtocols, parent.RespStatus)
	assert.Equal(t, "GET", parent.Method)
	assert.True(t, strings.HasPrefix(parent.URL, "wss://"))
	assert.Contains(t, parent.URL, "/ws")
	assert.Equal(t, parent.ID, deref(c2s.ParentID))
	assert.Equal(t, parent.ID, deref(s2c.ParentID))

	assert.True(t, c2s.IsWSMessage)
	assert.Equal(t, "text", deref(c2s.MessageType))
	assert.Equal(t, "c2s", deref(c2s.Direction))
	assert.Equal(t, []byte("ping from client"), c2s.ReqBody)

	assert.True(t, s2c.IsWSMessage)
	assert.Equal(t, "text", deref(s2c.MessageType))
	assert.Equal(t, "s2c", deref(s2c.Direction))
	assert.Equal(t, []byte("echo: ping from client"), s2c.RespBody)
}

// newEchoWSServer returns an httptest TLS server whose only handler is a
// WebSocket echo. Frames received are echoed back with an "echo: " prefix.
func newEchoWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Logf("upstream accept: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "test failure")

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		_ = c.Write(ctx, typ, append([]byte("echo: "), data...))
		c.Close(websocket.StatusNormalClosure, "")
	})
	return httptest.NewTLSServer(mux)
}

// fixedConnTransport is a minimal http.RoundTripper that returns a single
// pre-established net.Conn for any DialTLSContext call. coder/websocket uses
// the http.Client to send the upgrade request and inspect the 101; we wedge
// our existing TLS conn into its Dial path so the upgrade flows through the
// proxy we're testing.
type fixedConnTransport struct {
	conn net.Conn
	host string
}

func (f *fixedConnTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr := &http.Transport{
		DialTLSContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return f.conn, nil
		},
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return f.conn, nil
		},
		// Disable HTTP/2 so the WS handshake stays on the same TCP stream.
		ForceAttemptHTTP2: false,
	}
	return tr.RoundTrip(req)
}

func drainFlows(t *testing.T, out <-chan types.RawFlow, want int, timeout time.Duration) []types.RawFlow {
	t.Helper()
	deadline := time.After(timeout)
	flows := make([]types.RawFlow, 0, want)
	for len(flows) < want {
		select {
		case f := <-out:
			flows = append(flows, f)
		case <-deadline:
			t.Fatalf("only got %d flows, wanted %d", len(flows), want)
		}
	}
	return flows
}

func classifyHijackFlows(t *testing.T, flows []types.RawFlow) (parent, c2s, s2c types.RawFlow) {
	t.Helper()
	var found bool
	for _, f := range flows {
		switch {
		case f.IsWSMessage && f.Direction != nil && *f.Direction == "c2s":
			c2s = f
		case f.IsWSMessage && f.Direction != nil && *f.Direction == "s2c":
			s2c = f
		case !f.IsWSMessage:
			parent = f
			found = true
		}
	}
	if !found {
		t.Fatalf("missing parent upgrade flow in %+v", flows)
	}
	return
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// guard against accidental import drop of errors.
var _ = errors.New

// TestRunWSPumpDegradesOnReassemblyError exercises the message-level failure
// path: a continuation-without-start frame violates the reassembler invariant.
// The pump must emit a ws_parse_error event, stop persisting messages, but
// keep forwarding frames so the underlying WS session stays alive.
func TestRunWSPumpDegradesOnReassemblyError(t *testing.T) {
	srcR, srcW := io.Pipe()
	defer srcW.Close()
	defer srcR.Close()

	var dst syncBuffer
	out := make(chan types.RawFlow, 8)
	opts := Options{
		Out:         out,
		IDGen:       idgen.NewGenerator(),
		CaptureMode: "permissive",
		Logger:      func(string, ...any) {},
	}

	const parentID = "01HZTESTPARENT0000000"
	pumpDone := make(chan struct{})
	go func() {
		runWSPump(opts, parentID, "c2s", bufio.NewReader(srcR), &dst)
		close(pumpDone)
	}()

	writeAndWait := func(f *wsFrame) {
		require.NoError(t, writeFrame(srcW, f))
	}

	writeAndWait(&wsFrame{Fin: true, Opcode: opText, Payload: []byte("hi")})
	first := receiveFlow(t, out, "first text frame")
	require.True(t, first.IsWSMessage)
	require.Equal(t, "text", deref(first.MessageType))

	writeAndWait(&wsFrame{Fin: true, Opcode: opContinuation, Payload: []byte("orphan")})
	parseErr := receiveFlow(t, out, "ws_parse_error after orphan continuation")
	require.True(t, parseErr.IsWSMessage)
	require.Equal(t, "control", deref(parseErr.MessageType))
	require.Equal(t, "ws_parse_error", deref(parseErr.ControlOp))

	writeAndWait(&wsFrame{Fin: true, Opcode: opText, Payload: []byte("after")})
	select {
	case f := <-out:
		t.Fatalf("expected no further events after parse error, got %+v", f)
	case <-time.After(150 * time.Millisecond):
	}

	writeAndWait(&wsFrame{Fin: true, Opcode: opClose, Payload: []byte{0x03, 0xe8}})
	closeEv := receiveFlow(t, out, "close event")
	require.Equal(t, "close", deref(closeEv.ControlOp))

	select {
	case <-pumpDone:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not exit after CLOSE")
	}

	require.Greater(t, dst.Len(), 0, "expected forwarded bytes after parse error")
}

func receiveFlow(t *testing.T, out <-chan types.RawFlow, label string) types.RawFlow {
	t.Helper()
	select {
	case f := <-out:
		return f
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return types.RawFlow{}
	}
}

// syncBuffer is a goroutine-safe byte sink for the pump's downstream writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *syncBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}
