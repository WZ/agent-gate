package proxy

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA

	defaultMaxWSMessageBytes = 16 * 1024 * 1024
)

type wsFrame struct {
	Fin     bool
	Opcode  byte
	Mask    bool
	Length  uint64
	Payload []byte
}

func readFrame(r io.Reader) (*wsFrame, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read websocket header: %w", err)
	}

	if header[0]&0x70 != 0 {
		return nil, errors.New("websocket frame uses unsupported RSV bits")
	}
	fin := header[0]&0x80 != 0
	opcode := header[0] & 0x0F
	if !validOpcode(opcode) {
		return nil, fmt.Errorf("websocket frame has invalid opcode 0x%x", opcode)
	}

	masked := header[1]&0x80 != 0
	lengthCode := header[1] & 0x7F
	length := uint64(lengthCode)
	switch lengthCode {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, fmt.Errorf("read websocket 16-bit length: %w", err)
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, fmt.Errorf("read websocket 64-bit length: %w", err)
		}
		length = binary.BigEndian.Uint64(ext[:])
		if length&(1<<63) != 0 {
			return nil, errors.New("websocket 64-bit length has high bit set")
		}
	}

	if isControlOpcode(opcode) {
		if !fin {
			return nil, errors.New("websocket control frame is fragmented")
		}
		if length > 125 {
			return nil, errors.New("websocket control frame payload exceeds 125 bytes")
		}
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, fmt.Errorf("read websocket mask key: %w", err)
		}
	}

	maxInt := uint64(int(^uint(0) >> 1))
	if length > maxInt {
		return nil, fmt.Errorf("websocket payload too large for memory: %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read websocket payload: %w", err)
	}
	if masked {
		maskPayload(payload, maskKey)
	}

	return &wsFrame{
		Fin:     fin,
		Opcode:  opcode,
		Mask:    masked,
		Length:  length,
		Payload: payload,
	}, nil
}

func writeFrame(w io.Writer, f *wsFrame) error {
	if f == nil {
		return errors.New("nil websocket frame")
	}
	if !validOpcode(f.Opcode) {
		return fmt.Errorf("websocket frame has invalid opcode 0x%x", f.Opcode)
	}
	length := uint64(len(f.Payload))
	if isControlOpcode(f.Opcode) {
		if !f.Fin {
			return errors.New("websocket control frame is fragmented")
		}
		if length > 125 {
			return errors.New("websocket control frame payload exceeds 125 bytes")
		}
	}

	first := f.Opcode
	if f.Fin {
		first |= 0x80
	}
	second := byte(0)
	if f.Mask {
		second |= 0x80
	}

	header := []byte{first, second}
	switch {
	case length <= 125:
		header[1] |= byte(length)
	case length <= 0xFFFF:
		header[1] |= 126
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(length))
		header = append(header, ext[:]...)
	default:
		header[1] |= 127
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], length)
		header = append(header, ext[:]...)
	}
	if _, err := w.Write(header); err != nil {
		return err
	}

	payload := f.Payload
	if f.Mask {
		var key [4]byte
		if _, err := rand.Read(key[:]); err != nil {
			return fmt.Errorf("generate websocket mask key: %w", err)
		}
		if _, err := w.Write(key[:]); err != nil {
			return err
		}
		payload = append([]byte(nil), f.Payload...)
		maskPayload(payload, key)
	}
	_, err := w.Write(payload)
	return err
}

type reassembler struct {
	accum     []byte
	opcode    byte
	maxBytes  int
	truncated bool
}

func (r *reassembler) Append(f *wsFrame) (msgBody []byte, msgType byte, complete bool, oversize bool, err error) {
	if f == nil {
		return nil, 0, false, false, errors.New("nil websocket frame")
	}
	if isControlOpcode(f.Opcode) {
		return nil, 0, false, false, nil
	}

	switch f.Opcode {
	case opText, opBinary:
		if r.opcode != 0 {
			return nil, 0, false, false, errors.New("websocket nested data frame before continuation completed")
		}
		r.opcode = f.Opcode
	case opContinuation:
		if r.opcode == 0 {
			return nil, 0, false, false, errors.New("websocket continuation without start frame")
		}
	default:
		return nil, 0, false, false, fmt.Errorf("websocket frame has invalid opcode 0x%x", f.Opcode)
	}

	oversize = r.appendPayload(f.Payload)
	if !f.Fin {
		return nil, r.opcode, false, oversize, nil
	}

	body := append([]byte(nil), r.accum...)
	return body, r.opcode, true, r.truncated || oversize, nil
}

func (r *reassembler) Reset() {
	r.accum = nil
	r.opcode = 0
	r.truncated = false
}

func (r *reassembler) appendPayload(payload []byte) bool {
	maxBytes := r.max()
	if len(r.accum) >= maxBytes {
		if len(payload) > 0 {
			r.truncated = true
		}
		return r.truncated
	}
	remaining := maxBytes - len(r.accum)
	if len(payload) > remaining {
		r.accum = append(r.accum, payload[:remaining]...)
		r.truncated = true
		return true
	}
	r.accum = append(r.accum, payload...)
	return r.truncated
}

func (r *reassembler) max() int {
	if r.maxBytes <= 0 {
		return defaultMaxWSMessageBytes
	}
	return r.maxBytes
}

func validOpcode(opcode byte) bool {
	switch opcode {
	case opContinuation, opText, opBinary, opClose, opPing, opPong:
		return true
	default:
		return false
	}
}

func isControlOpcode(opcode byte) bool {
	return opcode == opClose || opcode == opPing || opcode == opPong
}

func maskPayload(payload []byte, key [4]byte) {
	for i := range payload {
		payload[i] ^= key[i%4]
	}
}
