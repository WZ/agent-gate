package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"agent-gate/internal/types"
)

// runHijack owns the raw client TCP conn for a CONNECT that matched
// HijackHost. It TLS-terminates with our local CA, dials upstream, then
// loops reading HTTP/1.1 requests off the client (keepalive). For each
// request:
//
//   - If it's a WebSocket Upgrade: forward the upgrade, persist it as a
//     parent flow, and run bidirectional frame-aware pumps that emit
//     per-message events linked via parent_id. WebSocket sessions are
//     terminal — once a conn upgrades, no more HTTP requests can ride
//     on it, so the function returns when the pumps drain.
//
//   - Otherwise: forward as a normal HTTP request, capture req+resp
//     bodies, emit a RawFlow that looks identical to one the standard
//     MITM path would have produced, then loop for the next request on
//     the same conn. This is what keeps the hijack predicate safe to
//     enable on hosts that mix HTTP and WebSocket traffic (codex's
//     chatgpt.com is the canonical case — setup endpoints over HTTP,
//     model invocation over WebSockets, often on the same TCP conn).
func runHijack(opts Options, hostport, serverName string, client net.Conn) {
	defer client.Close()

	if _, err := client.Write([]byte("HTTP/1.0 200 Connection established\r\n\r\n")); err != nil {
		opts.Logger("proxy hijack: write 200 to client: %v", err)
		return
	}

	tlsCfg, err := opts.CA.LeafSignerFunc(serverName)
	if err != nil {
		opts.Logger("proxy hijack: leaf sign for %s: %v", serverName, err)
		return
	}
	tlsClient := tls.Server(client, tlsCfg)
	if err := tlsClient.Handshake(); err != nil {
		opts.Logger("proxy hijack: tls handshake with client: %v", err)
		return
	}
	defer tlsClient.Close()

	clientReader := bufio.NewReader(tlsClient)

	// Upstream is dialed lazily on the first request that isn't blocked.
	// Blocked-by-HostGuard CONNECTs never touch the upstream — saves a TCP
	// round-trip and matches the standard MITM path's "block before forward"
	// posture.
	var (
		tlsUpstream    *tls.Conn
		upstreamReader *bufio.Reader
	)
	defer func() {
		if tlsUpstream != nil {
			_ = tlsUpstream.Close()
		}
	}()

	for {
		req, err := http.ReadRequest(clientReader)
		if err != nil {
			if !isPumpClose(err) {
				opts.Logger("proxy hijack: read request from %s: %v", serverName, err)
			}
			return
		}

		if opts.HostGuard != nil {
			host := req.URL.Hostname()
			if host == "" {
				host = serverName
			}
			if opts.HostGuard(host) {
				if err := writeBlockedHijackResponse(opts, serverName, host, req, tlsClient); err != nil && !isPumpClose(err) {
					opts.Logger("proxy hijack: write 403 for %s: %v", host, err)
				}
				return
			}
		}

		if tlsUpstream == nil {
			tlsUpstream, err = dialUpstreamTLS(opts, hostport, serverName)
			if err != nil {
				opts.Logger("proxy hijack: %v", err)
				return
			}
			upstreamReader = bufio.NewReader(tlsUpstream)
		}

		if isWebSocketUpgrade(req) {
			handleHijackedWSUpgrade(opts, serverName, req, clientReader, upstreamReader, tlsClient, tlsUpstream)
			return
		}

		if err := forwardHijackedHTTP(opts, serverName, req, upstreamReader, tlsClient, tlsUpstream); err != nil {
			if !isPumpClose(err) {
				opts.Logger("proxy hijack: forward HTTP %s %s: %v", req.Method, req.URL.RequestURI(), err)
			}
			return
		}
	}
}

// writeBlockedHijackResponse mirrors the standard MITM block path: emit a
// RawFlow with status 403 and the synthetic body, then write the same
// response back to the client. The response sets Connection: close because
// we always terminate the hijack tunnel after a block — keeping it open
// would let the agent retry the same blocked path silently.
func writeBlockedHijackResponse(opts Options, serverName, host string, req *http.Request, tlsClient *tls.Conn) error {
	reqBody, _, _ := readLimited(req.Body, opts.BodyLimit)
	if req.Body != nil {
		req.Body.Close()
	}

	body := blockedHostBody(host)
	headers := http.Header{
		"Content-Type":       []string{"application/json"},
		"X-Agent-Gate-Block": []string{"host_not_allowlisted"},
		"Connection":         []string{"close"},
	}

	now := time.Now()
	flow := types.RawFlow{
		ID:          opts.IDGen.New(),
		StartedAt:   now,
		EndedAt:     now,
		Method:      req.Method,
		URL:         hijackedHTTPURL(req, serverName),
		ReqHeaders:  req.Header.Clone(),
		ReqBody:     reqBody,
		RespStatus:  http.StatusForbidden,
		RespHeaders: headers.Clone(),
		RespBody:    body,
		CaptureMode: opts.CaptureMode,
	}
	opts.Out <- flow

	resp := &http.Response{
		Status:        "403 Forbidden",
		StatusCode:    http.StatusForbidden,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        headers,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Close:         true,
		Request:       req,
	}
	return resp.Write(tlsClient)
}

func dialUpstreamTLS(opts Options, hostport, serverName string) (*tls.Conn, error) {
	upstreamCfg := &tls.Config{
		ServerName:         serverName,
		RootCAs:            opts.UpstreamRootCAs,
		InsecureSkipVerify: opts.UpstreamInsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	rawUpstream, err := net.DialTimeout("tcp", hostport, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial upstream %s: %w", hostport, err)
	}
	tlsUpstream := tls.Client(rawUpstream, upstreamCfg)
	if err := tlsUpstream.Handshake(); err != nil {
		rawUpstream.Close()
		return nil, fmt.Errorf("upstream handshake %s: %w", serverName, err)
	}
	return tlsUpstream, nil
}

// handleHijackedWSUpgrade forwards the upgrade request upstream, writes the
// response back to the client, persists the upgrade as the parent flow, and
// — on 101 — runs the bidirectional frame pumps until both directions exit.
func handleHijackedWSUpgrade(
	opts Options,
	serverName string,
	req *http.Request,
	clientReader, upstreamReader *bufio.Reader,
	tlsClient, tlsUpstream *tls.Conn,
) {
	upstreamReq := upstreamUpgradeRequest(req)
	if err := upstreamReq.Write(tlsUpstream); err != nil {
		opts.Logger("proxy hijack: forward upgrade request: %v", err)
		return
	}

	resp, err := http.ReadResponse(upstreamReader, upstreamReq)
	if err != nil {
		opts.Logger("proxy hijack: read upgrade response: %v", err)
		return
	}

	startedAt := time.Now()
	parentID := opts.IDGen.New()
	emitUpgradeFlow(opts, parentID, serverName, req, resp, startedAt)

	if err := writeResponse(tlsClient, resp); err != nil {
		opts.Logger("proxy hijack: write upgrade response to client: %v", err)
		return
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		// Upstream refused the upgrade; the conn is now in an undefined
		// state — codex won't try to send more HTTP on it. Return.
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		runWSPump(opts, parentID, "c2s", clientReader, tlsUpstream)
		_ = tlsUpstream.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		runWSPump(opts, parentID, "s2c", upstreamReader, tlsClient)
		_ = tlsClient.CloseWrite()
	}()
	wg.Wait()
}

func upstreamUpgradeRequest(req *http.Request) *http.Request {
	upstreamReq := req.Clone(req.Context())
	upstreamReq.Header = req.Header.Clone()
	// The frame reader does not implement permessage-deflate. Do not allow the
	// upstream to negotiate RSV1-compressed frames that the pump will reject.
	upstreamReq.Header.Del("Sec-Websocket-Extensions")
	return upstreamReq
}

// forwardHijackedHTTP forwards a single non-WS HTTP/1.1 request to upstream,
// captures the request and response bodies, emits a RawFlow that mirrors
// what the standard MITM path produces, then writes the response back to
// the client. Returns an error to stop the request loop (e.g. on
// `Connection: close`), nil to keep looping.
func forwardHijackedHTTP(
	opts Options,
	serverName string,
	req *http.Request,
	upstreamReader *bufio.Reader,
	tlsClient, tlsUpstream *tls.Conn,
) error {
	startedAt := time.Now()
	id := opts.IDGen.New()

	reqBody, reqTrunc, _ := readLimited(req.Body, opts.BodyLimit)
	if req.Body != nil {
		req.Body.Close()
	}
	if reqBody != nil {
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
		req.ContentLength = int64(len(reqBody))
	} else {
		req.Body = http.NoBody
		req.ContentLength = 0
	}
	reqHeaders := req.Header.Clone()

	if err := req.Write(tlsUpstream); err != nil {
		return fmt.Errorf("write request upstream: %w", err)
	}

	resp, err := http.ReadResponse(upstreamReader, req)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	respBody, respTrunc, _ := readLimited(resp.Body, opts.BodyLimit)
	if resp.Body != nil {
		resp.Body.Close()
	}
	respHeaders := resp.Header.Clone()
	if respBody != nil {
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		resp.ContentLength = int64(len(respBody))
	} else {
		resp.Body = http.NoBody
		resp.ContentLength = 0
	}

	flow := types.RawFlow{
		ID:            id,
		StartedAt:     startedAt,
		EndedAt:       time.Now(),
		Method:        req.Method,
		URL:           hijackedHTTPURL(req, serverName),
		ReqHeaders:    reqHeaders,
		ReqBody:       reqBody,
		RespStatus:    resp.StatusCode,
		RespHeaders:   respHeaders,
		RespBody:      respBody,
		IsStreamed:    isSSE(respHeaders),
		BodyTruncated: reqTrunc || respTrunc,
		CaptureMode:   opts.CaptureMode,
	}
	opts.Out <- flow

	if err := resp.Write(tlsClient); err != nil {
		return fmt.Errorf("write response to client: %w", err)
	}

	if shouldCloseAfter(req, resp) {
		return io.EOF
	}
	return nil
}

func hijackedHTTPURL(req *http.Request, serverName string) string {
	u := &url.URL{
		Scheme: "https",
		Host:   serverName,
		Path:   req.URL.Path,
	}
	if req.URL.RawQuery != "" {
		u.RawQuery = req.URL.RawQuery
	}
	return u.String()
}

func shouldCloseAfter(req *http.Request, resp *http.Response) bool {
	if req.ProtoAtLeast(1, 1) {
		// HTTP/1.1 default is keepalive; explicit Connection: close on
		// either side terminates.
		if headerHasToken(req.Header, "Connection", "close") ||
			headerHasToken(resp.Header, "Connection", "close") {
			return true
		}
		return false
	}
	// HTTP/1.0 default is close; only keep-alive opts in.
	if headerHasToken(req.Header, "Connection", "keep-alive") &&
		headerHasToken(resp.Header, "Connection", "keep-alive") {
		return false
	}
	return true
}

// runWSPump reads RFC 6455 frames from src, forwards them to dst, and emits
// per-message events linked to parentID. The reassembler enforces the 16 MB
// cap; oversize messages are persisted truncated with an oversize flag.
//
// Failure modes:
//   - readFrame error mid-frame (byte stream desynced): emit ws_parse_error,
//     close the pump. The connection ends; the agent's next session re-attempts
//     capture from scratch. Strict raw passthrough mid-frame isn't safe because
//     we may have consumed partial bytes for the malformed frame.
//   - reassembler protocol violation (e.g. nested START before continuation
//     completes): emit ws_parse_error and stop persisting messages on this
//     pump, but keep forwarding frames. The byte stream is still in sync —
//     only the message-level invariant broke — so the agent's session can
//     continue even though body capture stops.
func runWSPump(opts Options, parentID, direction string, src *bufio.Reader, dst io.Writer) {
	re := &reassembler{}
	persistMessages := true
	for {
		f, err := readFrame(src)
		if err != nil {
			if !isPumpClose(err) {
				opts.Logger("proxy hijack pump %s: read frame: %v", direction, err)
				emitWSError(opts, parentID, direction, "ws_read_error", err)
			}
			return
		}

		if err := writeFrame(dst, f); err != nil {
			if !isPumpClose(err) {
				opts.Logger("proxy hijack pump %s: write frame: %v", direction, err)
			}
			return
		}

		if isControlOpcode(f.Opcode) {
			emitControlMessage(opts, parentID, direction, f)
			if f.Opcode == opClose {
				return
			}
			continue
		}

		if !persistMessages {
			continue
		}

		body, msgType, complete, oversize, err := re.Append(f)
		if err != nil {
			opts.Logger("proxy hijack pump %s: re-assemble: %v", direction, err)
			emitWSError(opts, parentID, direction, "ws_parse_error", err)
			re.Reset()
			persistMessages = false
			continue
		}
		if !complete {
			continue
		}
		emitDataMessage(opts, parentID, direction, msgType, body, oversize)
		re.Reset()
	}
}

func emitUpgradeFlow(opts Options, id, serverName string, req *http.Request, resp *http.Response, startedAt time.Time) {
	respBody, _, _ := readLimited(resp.Body, opts.BodyLimit)
	if resp.Body != nil {
		resp.Body.Close()
	}
	if respBody != nil {
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}

	flow := types.RawFlow{
		ID:          id,
		StartedAt:   startedAt,
		EndedAt:     time.Now(),
		Method:      req.Method,
		URL:         upgradeURLString(req, serverName),
		ReqHeaders:  req.Header.Clone(),
		RespStatus:  resp.StatusCode,
		RespHeaders: resp.Header.Clone(),
		RespBody:    respBody,
		CaptureMode: opts.CaptureMode,
	}
	opts.Out <- flow
}

func emitDataMessage(opts Options, parentID, direction string, msgType byte, body []byte, oversize bool) {
	mt := messageTypeString(msgType)
	dir := direction
	now := time.Now()
	flow := types.RawFlow{
		ID:          opts.IDGen.New(),
		StartedAt:   now,
		EndedAt:     now,
		CaptureMode: opts.CaptureMode,
		ParentID:    &parentID,
		MessageType: &mt,
		Direction:   &dir,
		IsWSMessage: true,
	}
	switch direction {
	case "c2s":
		flow.ReqBody = body
	case "s2c":
		flow.RespBody = body
	}
	if oversize {
		flow.BodyTruncated = true
	}
	opts.Out <- flow
}

func emitControlMessage(opts Options, parentID, direction string, f *wsFrame) {
	mt := "control"
	dir := direction
	op := controlOpString(f.Opcode)
	now := time.Now()
	flow := types.RawFlow{
		ID:          opts.IDGen.New(),
		StartedAt:   now,
		EndedAt:     now,
		CaptureMode: opts.CaptureMode,
		ParentID:    &parentID,
		MessageType: &mt,
		Direction:   &dir,
		IsWSMessage: true,
		ControlOp:   &op,
	}
	if f.Opcode == opClose && len(f.Payload) >= 2 {
		code := int(uint16(f.Payload[0])<<8 | uint16(f.Payload[1]))
		flow.CloseCode = &code
		if len(f.Payload) > 2 {
			reason := append([]byte(nil), f.Payload[2:]...)
			switch direction {
			case "c2s":
				flow.ReqBody = reason
			case "s2c":
				flow.RespBody = reason
			}
		}
	}
	opts.Out <- flow
}

func emitWSError(opts Options, parentID, direction, code string, err error) {
	mt := "control"
	dir := direction
	op := code
	now := time.Now()
	flow := types.RawFlow{
		ID:          opts.IDGen.New(),
		StartedAt:   now,
		EndedAt:     now,
		CaptureMode: opts.CaptureMode,
		ParentID:    &parentID,
		MessageType: &mt,
		Direction:   &dir,
		IsWSMessage: true,
		ControlOp:   &op,
		Err:         err.Error(),
	}
	opts.Out <- flow
}

func writeResponse(w io.Writer, resp *http.Response) error {
	if err := resp.Write(w); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func upgradeURLString(req *http.Request, serverName string) string {
	u := &url.URL{
		Scheme: "wss",
		Host:   serverName,
		Path:   req.URL.Path,
	}
	if req.URL.RawQuery != "" {
		u.RawQuery = req.URL.RawQuery
	}
	return u.String()
}

func isWebSocketUpgrade(req *http.Request) bool {
	if !strings.EqualFold(req.Method, "GET") {
		return false
	}
	if !headerHasToken(req.Header, "Connection", "upgrade") {
		return false
	}
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return true
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for part := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func messageTypeString(opcode byte) string {
	switch opcode {
	case opText:
		return "text"
	case opBinary:
		return "binary"
	default:
		return "unknown"
	}
}

func controlOpString(opcode byte) string {
	switch opcode {
	case opClose:
		return "close"
	case opPing:
		return "ping"
	case opPong:
		return "pong"
	default:
		return fmt.Sprintf("opcode_0x%x", opcode)
	}
}

func isPumpClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	// tls.Conn.Close after the peer is gone surfaces as a wrapped EOF.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "connection reset by peer") {
		return true
	}
	return false
}
