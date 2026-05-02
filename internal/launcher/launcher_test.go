package launcher

import (
	"context"
	"testing"
)

func TestRun_RejectsEmptyCmd(t *testing.T) {
	_, err := Run(context.Background(), Options{Mode: Permissive})
	if err == nil {
		t.Fatal("want error for empty Cmd, got nil")
	}
}

func TestErrUnsupported_IsExported(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("ErrUnsupported should be a non-nil sentinel")
	}
}
