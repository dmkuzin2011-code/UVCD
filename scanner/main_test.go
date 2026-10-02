package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsageText(t *testing.T) {
	u := usageText()
	if !strings.Contains(u, "all") || !strings.Contains(u, "exit") {
		t.Fatalf("usageText missing commands: %q", u)
	}
}

func TestResponseMarshals(t *testing.T) {
	resp := Response{
		Status:  "ok",
		Command: "all",
		Categories: []Category{
			{Category: "Test", Count: 1, Columns: []string{"A"}, Rows: [][]string{{"x"}}},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Response
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Status != "ok" || len(back.Categories) != 1 {
		t.Fatalf("round trip mismatch: %+v", back)
	}
}

func TestCategoriesSummarySorts(t *testing.T) {
	cats := []Category{
		{Category: "B", Count: 2},
		{Category: "A", Count: 3},
		{Category: "C", Count: 0},
	}
	got := categoriesSummary(cats)
	if got != "2 b | 3 a" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[float64]string{
		0:        "0 B",
		1023:     "1023 B",
		1024:     "1.0 KiB",
		5 * GiB:  "5.0 GiB",
	}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Fatalf("humanSize(%v) = %q, want %q", in, got, want)
		}
	}
}

const GiB = 1024 * 1024 * 1024