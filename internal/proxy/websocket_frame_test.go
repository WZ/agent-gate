package proxy

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFrameDecodesMaskedShortText(t *testing.T) {
	raw := maskedFrameBytes(opText, true, []byte("hello"), [4]byte{1, 2, 3, 4})

	f, err := readFrame(bytes.NewReader(raw))
	require.NoError(t, err)

	assert.True(t, f.Fin)
	assert.Equal(t, byte(opText), f.Opcode)
	assert.True(t, f.Mask)
	assert.Equal(t, uint64(5), f.Length)
	assert.Equal(t, []byte("hello"), f.Payload)
}

func TestWriteFrameRoundTripsLengthVariants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		opcode byte
		mask   bool
	}{
		{name: "short", size: 5, opcode: opText},
		{name: "sixteen_bit", size: 126, opcode: opBinary},
		{name: "sixty_four_bit", size: 66000, opcode: opBinary},
		{name: "masked", size: 130, opcode: opText, mask: true},
		{name: "continuation", size: 5, opcode: opContinuation},
		{name: "close", size: 2, opcode: opClose},
		{name: "ping", size: 4, opcode: opPing},
		{name: "pong", size: 4, opcode: opPong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte{0x41}, tc.size)
			var buf bytes.Buffer
			require.NoError(t, writeFrame(&buf, &wsFrame{
				Fin:     true,
				Opcode:  tc.opcode,
				Mask:    tc.mask,
				Length:  uint64(len(payload)),
				Payload: payload,
			}))

			f, err := readFrame(&buf)
			require.NoError(t, err)
			assert.True(t, f.Fin)
			assert.Equal(t, tc.opcode, f.Opcode)
			assert.Equal(t, tc.mask, f.Mask)
			assert.Equal(t, uint64(tc.size), f.Length)
			assert.Equal(t, payload, f.Payload)
		})
	}
}

func TestReadFrameRejectsMalformedFrames(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "truncated header", raw: []byte{0x81}},
		{name: "rsv bit set", raw: []byte{0xC1, 0x00}},
		{name: "invalid opcode", raw: []byte{0x83, 0x00}},
		{name: "fragmented control", raw: []byte{0x09, 0x00}},
		{name: "control too long", raw: append([]byte{0x89, 126, 0, 126}, bytes.Repeat([]byte{'x'}, 126)...)},
		{name: "truncated mask", raw: []byte{0x81, 0x80, 1, 2}},
		{name: "truncated payload", raw: []byte{0x81, 0x05, 'h', 'e'}},
		{name: "invalid 64 bit length", raw: invalid64BitLengthFrame()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readFrame(bytes.NewReader(tc.raw))
			require.Error(t, err)
		})
	}
}

func TestReassemblerCompletesSingleAndFragmentedMessages(t *testing.T) {
	var r reassembler
	r.maxBytes = 64

	body, msgType, complete, oversize, err := r.Append(&wsFrame{
		Fin:     true,
		Opcode:  opText,
		Payload: []byte("hello"),
	})
	require.NoError(t, err)
	assert.True(t, complete)
	assert.False(t, oversize)
	assert.Equal(t, byte(opText), msgType)
	assert.Equal(t, []byte("hello"), body)
	r.Reset()

	body, _, complete, oversize, err = r.Append(&wsFrame{
		Fin:     false,
		Opcode:  opText,
		Payload: []byte("hel"),
	})
	require.NoError(t, err)
	assert.False(t, complete)
	assert.False(t, oversize)
	assert.Nil(t, body)

	body, msgType, complete, oversize, err = r.Append(&wsFrame{
		Fin:     true,
		Opcode:  opContinuation,
		Payload: []byte("lo"),
	})
	require.NoError(t, err)
	assert.True(t, complete)
	assert.False(t, oversize)
	assert.Equal(t, byte(opText), msgType)
	assert.Equal(t, []byte("hello"), body)
}

func TestReassemblerIgnoresControlFrames(t *testing.T) {
	var r reassembler
	r.maxBytes = 64

	_, _, complete, _, err := r.Append(&wsFrame{Fin: true, Opcode: opPing, Payload: []byte("x")})
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestReassemblerRejectsProtocolViolations(t *testing.T) {
	var r reassembler
	r.maxBytes = 64

	_, _, _, _, err := r.Append(&wsFrame{Fin: true, Opcode: opContinuation, Payload: []byte("orphan")})
	require.Error(t, err)

	r.Reset()
	_, _, _, _, err = r.Append(&wsFrame{Fin: false, Opcode: opText, Payload: []byte("hel")})
	require.NoError(t, err)
	_, _, _, _, err = r.Append(&wsFrame{Fin: true, Opcode: opBinary, Payload: []byte("nested")})
	require.Error(t, err)
}

func TestReassemblerTruncatesOversizedMessages(t *testing.T) {
	r := reassembler{maxBytes: 5}

	body, msgType, complete, oversize, err := r.Append(&wsFrame{
		Fin:     true,
		Opcode:  opText,
		Payload: []byte("hello world"),
	})
	require.NoError(t, err)
	assert.True(t, complete)
	assert.True(t, oversize)
	assert.Equal(t, byte(opText), msgType)
	assert.Equal(t, []byte("hello"), body)
}

func maskedFrameBytes(opcode byte, fin bool, payload []byte, key [4]byte) []byte {
	var first byte = opcode
	if fin {
		first |= 0x80
	}
	out := []byte{first, 0x80 | byte(len(payload))}
	out = append(out, key[:]...)
	for i, b := range payload {
		out = append(out, b^key[i%4])
	}
	return out
}

func invalid64BitLengthFrame() []byte {
	var raw bytes.Buffer
	raw.Write([]byte{0x82, 127})
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], 1<<63)
	raw.Write(b[:])
	return raw.Bytes()
}
