package bodydecode

import (
	"bytes"
	"net/http"
	"testing"

	"agent-gate/internal/types"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestCapsDecodedZstdBody(t *testing.T) {
	plain := bytes.Repeat([]byte("a"), maxDecodedBodyBytes+1)

	var compressed bytes.Buffer
	enc, err := zstd.NewWriter(&compressed)
	require.NoError(t, err)
	_, err = enc.Write(plain)
	require.NoError(t, err)
	require.NoError(t, enc.Close())

	got := Request(&types.RawFlow{
		ReqHeaders: http.Header{"Content-Encoding": []string{"zstd"}},
		ReqBody:    compressed.Bytes(),
	})

	assert.Len(t, got, maxDecodedBodyBytes)
}
