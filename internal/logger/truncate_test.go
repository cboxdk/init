package logger

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestRetainedMessageTruncationKeepsValidUTF8: the retained copy was cut on a
// byte index, which splits a multi-byte rune whenever the boundary lands inside
// one. The tail renders as a replacement character, and because the API
// serialises these entries as JSON, the result is invalid UTF-8 that a strict
// client rejects outright.
func TestRetainedMessageTruncationKeepsValidUTF8(t *testing.T) {
	// "日" is three bytes, so a cut at MaxStoredMessageBytes lands mid-rune for
	// two out of every three offsets.
	for _, pad := range []int{0, 1, 2} {
		body := strings.Repeat("a", pad) + strings.Repeat("日", MaxStoredMessageBytes)

		buf := NewLogBuffer(4)
		buf.Add(LogEntry{ProcessName: "p", Message: body})

		got := buf.GetRecent(1)
		if len(got) != 1 {
			t.Fatalf("pad %d: expected 1 entry, got %d", pad, len(got))
		}

		if !utf8.ValidString(got[0].Message) {
			t.Errorf("pad %d: the retained message is not valid UTF-8; "+
				"JSON encoding of this entry produces mojibake or fails", pad)
		}
		if !strings.Contains(got[0].Message, "truncated") {
			t.Errorf("pad %d: the truncation marker is missing", pad)
		}
	}
}

// TestShortMessagesAreNotTruncated keeps the common path exact.
func TestShortMessagesAreNotTruncated(t *testing.T) {
	buf := NewLogBuffer(4)
	msg := "日本語のログ行"
	buf.Add(LogEntry{ProcessName: "p", Message: msg})

	if got := buf.GetRecent(1); got[0].Message != msg {
		t.Errorf("message = %q, want %q", got[0].Message, msg)
	}
}
