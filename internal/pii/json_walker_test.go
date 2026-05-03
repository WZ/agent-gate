package pii

import (
	"reflect"
	"testing"
)

func TestWalkerEmitsKeysAndValuesAtCorrectOffsets(t *testing.T) {
	body := []byte(`{"name":"Alice","age":42}`)
	got := walkTokens(body)
	want := []walkerToken{
		{kind: tokKey, start: 2, end: 6},      // name (between quotes)
		{kind: tokString, start: 9, end: 14},  // Alice
		{kind: tokKey, start: 17, end: 20},    // age
		{kind: tokNumber, start: 22, end: 24}, // 42
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkTokens:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestWalkerHandlesNestedObjects(t *testing.T) {
	body := []byte(`{"user":{"name":"Alice"}}`)
	got := walkTokens(body)
	want := []walkerToken{
		{kind: tokKey, start: 2, end: 6},      // user
		{kind: tokKey, start: 10, end: 14},    // name
		{kind: tokString, start: 17, end: 22}, // Alice
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkTokens:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestWalkerHandlesArraysOfObjects(t *testing.T) {
	body := []byte(`{"users":[{"name":"A"},{"name":"B"}]}`)
	got := walkTokens(body)
	// Byte layout (0-indexed): `users` at [2:7], inner `{` at 10, first
	// `name` at [12:16], first `A` at [19:20], second `name` at [25:29],
	// second `B` at [32:33].
	want := []walkerToken{
		{kind: tokKey, start: 2, end: 7},
		{kind: tokKey, start: 12, end: 16},
		{kind: tokString, start: 19, end: 20},
		{kind: tokKey, start: 25, end: 29},
		{kind: tokString, start: 32, end: 33},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkTokens:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestWalkerSkipsEscapedQuotesInsideStrings(t *testing.T) {
	body := []byte(`{"k":"a\"b"}`)
	got := walkTokens(body)
	want := []walkerToken{
		{kind: tokKey, start: 2, end: 3},     // k
		{kind: tokString, start: 6, end: 10}, // a\"b (raw bytes)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkTokens:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestWalkerRecoversFromMalformedInput(t *testing.T) {
	// Truncated mid-value — walker should yield what it could parse.
	body := []byte(`{"name":"Alice","age`)
	got := walkTokens(body)
	want := []walkerToken{
		{kind: tokKey, start: 2, end: 6},     // name
		{kind: tokString, start: 9, end: 14}, // Alice
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkTokens:\n got: %+v\nwant: %+v", got, want)
	}
}
