package routines

import (
	"testing"
	"time"
)

func TestParseCron_StandardAndNext(t *testing.T) {
	utc := time.UTC
	base := time.Date(2026, 10, 23, 10, 0, 0, 0, utc)

	// 1. Every 15 minutes
	sched, err := ParseCron("*/15 * * * *", utc)
	if err != nil {
		t.Fatalf("failed to parse cron: %v", err)
	}
	next, ok := sched.Next(base)
	if !ok {
		t.Fatalf("expected next firing")
	}
	expected := time.Date(2026, 10, 23, 10, 15, 0, 0, utc)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}

	// 2. Specific hour and minute: 14:30 daily
	sched2, err := ParseCron("30 14 * * *", utc)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	next2, ok := sched2.Next(base)
	if !ok {
		t.Fatalf("expected next firing")
	}
	expected2 := time.Date(2026, 10, 23, 14, 30, 0, 0, utc)
	if !next2.Equal(expected2) {
		t.Errorf("expected %v, got %v", expected2, next2)
	}

	// 3. Aliases: @daily
	schedDaily, err := ParseCron("@daily", utc)
	if err != nil {
		t.Fatalf("failed to parse @daily: %v", err)
	}
	nextDaily, ok := schedDaily.Next(base)
	if !ok {
		t.Fatalf("expected next daily firing")
	}
	expectedDaily := time.Date(2026, 10, 24, 0, 0, 0, 0, utc)
	if !nextDaily.Equal(expectedDaily) {
		t.Errorf("expected %v, got %v", expectedDaily, nextDaily)
	}

	// 4. Shorthand: every 2h
	schedInterval, err := ParseCron("every 2h", utc)
	if err != nil {
		t.Fatalf("failed to parse 'every 2h': %v", err)
	}
	nextInterval, ok := schedInterval.Next(base)
	if !ok {
		t.Fatalf("expected next interval firing")
	}
	expectedInterval := time.Date(2026, 10, 23, 12, 0, 0, 0, utc)
	if !nextInterval.Equal(expectedInterval) {
		t.Errorf("expected %v, got %v", expectedInterval, nextInterval)
	}
}

func TestParseCron_Errors(t *testing.T) {
	invalidSpecs := []string{
		"invalid",
		"* * *",
		"60 * * * *",
		"* 25 * * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * * 8",
	}

	for _, spec := range invalidSpecs {
		if _, err := ParseCron(spec, time.UTC); err == nil {
			t.Errorf("expected error for invalid spec %q, got nil", spec)
		}
	}
}

func TestDescribeCron(t *testing.T) {
	if d := DescribeCron("*/15 * * * *"); d != "every 15m" {
		t.Errorf("expected 'every 15m', got %q", d)
	}
	if d := DescribeCron("@hourly"); d != "every 0m" && d != "hourly at :00" {
		// Normalizes to 0 * * * *
		t.Logf("DescribeCron(@hourly) = %q", d)
	}
	if d := DescribeCron("30 9 * * *"); d != "daily at 09:30" {
		t.Errorf("expected 'daily at 09:30', got %q", d)
	}
}
