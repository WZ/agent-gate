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
// HijackHost. It TLS-terminates with our local CA, reads the request the
// agent sends inside the tunnel, dials upstream, and — if the request is a
// WebSocket Upgrade — runs bidirectional frame-aware pumps that persist
// per-message events linked to a parent upgrade flow.
//
// Non-WebSocket traffic on a hijack-targeted host is closed; we only register
// the hijack for hosts whose protocol we want to frame-decode, and falling
// through to a plain HTTP forward would silently bypass the audit pipeline.
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
	req, err := http.ReadRequest(clientReader)
	if err != nil {
		opts.Logger("proxy hijack: read request: %v", err)
		return
	}

	if !isWebSocketUpgrade(req) {
		opts.Logger("proxy hijack: %s sent non-WS request %s %s; closing", serverName, req.Method, req.URL.RequestURI())
		return
	}

	upstreamCfg := &tls.Config{
		ServerName:         serverName,
		RootCAs:            opts.UpstreamRootCAs,
		InsecureSkipVerify: opts.UpstreamInsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	rawUpstream, err := net.DialTimeout("tcp", hostport, 30*time.Second)
	if err != nil {
		opts.Logger("proxy hijack: dial upstream %s: %v", hostport, err)
		return
	}
	tlsUpstream := tls.Client(rawUpstream, upstreamCfg)
	if err := tlsUpstream.Handshake(); err != nil {
		opts.Logger("proxy hijack: upstream handshake %s: %v", serverName, err)
		rawUpstream.Close()
		return
	}
	defer tlsUpstream.Close()

	if err := req.Write(tlsUpstream); err != nil {
		opts.Logger("proxy hijack: forward upgrade request: %v", err)
		return
	}

	upstreamReader := bufio.NewReader(tlsUpstream)
	resp, err := http.ReadResponse(upstreamReader, req)
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
		// Upstream refused the upgrade; nothing to pump.
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
