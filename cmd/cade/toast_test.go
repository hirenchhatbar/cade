package main

import (
	"strings"
	"testing"
)

func TestWrap(t *testing.T) {
	if got := wrap("delete failed: permission denied", 16, 4); strings.Join(got, "|") != "delete failed:|permission|denied" {
		t.Fatalf("words: %q", got)
	}
	if got := wrap("/a/very/long/path", 6, 4); strings.Join(got, "|") != "/a/ver|y/long|/path" {
		t.Fatalf("long word: %q", got)
	}
	if got := wrap("a b c d e", 1, 2); strings.Join(got, "|") != "a|…" {
		t.Fatalf("cap: %q", got)
	}
}

func TestToastStack(t *testing.T) {
	a := &app{}
	for range maxToasts + 2 {
		a.notify(toastOK, "x")
	}
	if len(a.toasts) != maxToasts || a.toasts[0].id != 3 {
		t.Fatalf("stack %v", a.toasts)
	}
	a.expireToast(3)
	if len(a.toasts) != maxToasts-1 || a.toasts[0].id != 4 {
		t.Fatalf("expire %v", a.toasts)
	}
}
