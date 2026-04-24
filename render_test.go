package main

import (
	"testing"
	"time"
)

func TestFormatResetTimeUsesLocalTimezone(t *testing.T) {
	oldLocal := time.Local
	oldLocale := localeOverride
	defer func() {
		time.Local = oldLocal
		localeOverride = oldLocale
	}()

	time.Local = time.FixedZone("CST", 8*60*60)
	localeOverride = "zh"

	got := formatResetTime(time.Date(2026, 4, 24, 7, 5, 0, 0, time.UTC))
	if got != "15:05" {
		t.Fatalf("formatResetTime = %q, want local time 15:05", got)
	}
}

func TestFormatResetDateTimeUsesLocalTimezone(t *testing.T) {
	oldLocal := time.Local
	oldLocale := localeOverride
	defer func() {
		time.Local = oldLocal
		localeOverride = oldLocale
	}()

	time.Local = time.FixedZone("CST", 8*60*60)
	localeOverride = "zh"

	got := formatResetDateTime(time.Date(2026, 4, 24, 20, 30, 0, 0, time.UTC))
	if got != "4/25 4:30" {
		t.Fatalf("formatResetDateTime = %q, want local datetime 4/25 4:30", got)
	}
}
