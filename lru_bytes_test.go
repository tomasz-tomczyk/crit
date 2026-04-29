package main

import (
	"bytes"
	"testing"
)

func TestBytesLRU_GetPutHit(t *testing.T) {
	c := newBytesLRU(4)
	c.Put("a", []byte("alpha"))
	got, ok := c.Get("a")
	if !ok || !bytes.Equal(got, []byte("alpha")) {
		t.Errorf("Get(a)=(%q,%v), want (alpha,true)", got, ok)
	}
}

func TestBytesLRU_MissReportsFalse(t *testing.T) {
	c := newBytesLRU(4)
	if _, ok := c.Get("missing"); ok {
		t.Errorf("Get(missing) should miss")
	}
}

func TestBytesLRU_EvictsLeastRecentlyUsed(t *testing.T) {
	c := newBytesLRU(3)
	c.Put("a", []byte("1"))
	c.Put("b", []byte("2"))
	c.Put("c", []byte("3"))
	// Touch a so b becomes the LRU.
	if _, ok := c.Get("a"); !ok {
		t.Fatal("Get(a) miss — fixture broken")
	}
	c.Put("d", []byte("4"))

	if _, ok := c.Get("b"); ok {
		t.Errorf("b should have been evicted; cache len=%d", c.Len())
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(k); !ok {
			t.Errorf("%s should be retained", k)
		}
	}
}

func TestBytesLRU_PutOverwritesPromotes(t *testing.T) {
	c := newBytesLRU(2)
	c.Put("a", []byte("1"))
	c.Put("b", []byte("2"))
	// Overwrite a — must promote it past b.
	c.Put("a", []byte("11"))
	c.Put("c", []byte("3"))
	// b should be the evicted LRU.
	if _, ok := c.Get("b"); ok {
		t.Errorf("b should have been evicted on c insertion")
	}
	got, _ := c.Get("a")
	if !bytes.Equal(got, []byte("11")) {
		t.Errorf("a=%q want 11", got)
	}
}

func TestBytesLRU_ZeroCapClampsToOne(t *testing.T) {
	c := newBytesLRU(0)
	c.Put("a", []byte("x"))
	c.Put("b", []byte("y"))
	if c.Len() != 1 {
		t.Errorf("len=%d want 1 (cap clamped to 1)", c.Len())
	}
}
