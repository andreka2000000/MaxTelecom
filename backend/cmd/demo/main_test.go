package main

import (
	"os"
	"strings"
	"testing"
)

func TestDemoRebasesCompleteDataset(t *testing.T) {
	dir := "/data/synthetic"
	if _, e := os.Stat(dir); e != nil {
		dir = "../../../data/synthetic"
	}
	d, e := loadDemo(dir, "2026-11-16")
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Day.Orders) != 400 || len(d.Reference.Staff) != 40 || d.Day.Now != "2026-11-16T08:45:00+05:00" {
		t.Fatal("incomplete demo")
	}
	for _, o := range d.Day.Orders {
		if !strings.HasPrefix(o.OrderId, "ORD-20261116-") || !strings.HasPrefix(string(o.Slot.Start), "2026-11-16T") {
			t.Fatal("old dates remain", o.OrderId)
		}
	}
}
