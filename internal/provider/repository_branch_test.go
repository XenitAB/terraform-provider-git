package provider

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestComputeBranchName_NoTimestamp verifies that when append_timestamp is false
// the branch name is returned unchanged.
func TestComputeBranchName_NoTimestamp(t *testing.T) {
	result := computeBranchName("feature", false, defaultTimestampFormat)
	if result != "feature" {
		t.Errorf("expected %q, got %q", "feature", result)
	}
}

// TestComputeBranchName_WithTimestamp verifies that when append_timestamp is true
// a "-YYYYMMDDHHMMSS" suffix is appended.
func TestComputeBranchName_WithTimestamp(t *testing.T) {
	result := computeBranchName("feature", true, defaultTimestampFormat)

	if !strings.HasPrefix(result, "feature-") {
		t.Fatalf("expected result to start with %q, got %q", "feature-", result)
	}

	suffix := strings.TrimPrefix(result, "feature-")
	// The default format "20060102150405" produces exactly 14 digits.
	matched, err := regexp.MatchString(`^\d{14}$`, suffix)
	if err != nil {
		t.Fatalf("unexpected regexp error: %v", err)
	}
	if !matched {
		t.Errorf("timestamp suffix %q does not match YYYYMMDDHHMMSS (14 digits), got %q", "14-digit pattern", suffix)
	}
}

// TestComputeBranchName_CustomFormat verifies that a custom Go time layout is
// applied correctly.
func TestComputeBranchName_CustomFormat(t *testing.T) {
	const customFormat = "2006-01-02"
	before := time.Now().UTC()
	result := computeBranchName("release", true, customFormat)
	after := time.Now().UTC()

	if !strings.HasPrefix(result, "release-") {
		t.Fatalf("expected result to start with %q, got %q", "release-", result)
	}

	suffix := strings.TrimPrefix(result, "release-")

	// The suffix must parse as a valid date using the custom format.
	parsed, err := time.Parse(customFormat, suffix)
	if err != nil {
		t.Fatalf("could not parse suffix %q with format %q: %v", suffix, customFormat, err)
	}

	// The date must fall between before and after (truncated to day for this format).
	dayBefore := before.Format(customFormat)
	dayAfter := after.Format(customFormat)
	dayParsed := parsed.Format(customFormat)
	if dayParsed < dayBefore || dayParsed > dayAfter {
		t.Errorf("parsed date %q is not between %q and %q", dayParsed, dayBefore, dayAfter)
	}
}

// TestComputeBranchName_24HourFormat verifies that the default timestamp format
// uses a 24-hour clock (the Go reference hour is "15", not "3PM").
func TestComputeBranchName_24HourFormat(t *testing.T) {
	result := computeBranchName("test", true, defaultTimestampFormat)

	suffix := strings.TrimPrefix(result, "test-")

	// The default format "20060102150405" produces exactly 14 digits.
	matched, err := regexp.MatchString(`^\d{14}$`, suffix)
	if err != nil {
		t.Fatalf("unexpected regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("timestamp suffix %q does not match YYYYMMDDHHMMSS (14 digits), got %q", "14-digit pattern", suffix)
	}

	hourStr := suffix[8:10]
	if _, err := time.Parse("15", hourStr); err != nil {
		t.Errorf("hour portion %q is not a valid 24-hour value (00-23): %v", hourStr, err)
	}
}

// TestComputeBranchName_EmptyFormatFallback verifies that an empty format string
// falls back to defaultTimestampFormat.
func TestComputeBranchName_EmptyFormatFallback(t *testing.T) {
	result := computeBranchName("branch", true, "")
	suffix := strings.TrimPrefix(result, "branch-")
	matched, _ := regexp.MatchString(`^\d{14}$`, suffix)
	if !matched {
		t.Errorf("expected 14-digit suffix with empty format, got %q", suffix)
	}
}
