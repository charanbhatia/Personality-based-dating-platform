package matching

import (
	"encoding/base64"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
)

func mustEncode(t *testing.T, v any) string {
	t.Helper()
	raw, err := encodeCursor(v)
	if err != nil {
		t.Fatalf("encodeCursor: %v", err)
	}
	return raw
}

// assertBadRequest checks the error is the 400 the handler layer turns into a
// client-visible response, rather than an internal error that would surface a 500.
func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *httpx.Error", err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", apiErr.Status, http.StatusBadRequest)
	}
}

func TestDiscoverCursorRoundTrip(t *testing.T) {
	want := discoverCursor{Version: cursorVersion, Score: "0.846800", UserID: uuid.New()}
	got, err := parseDiscoverCursor(mustEncode(t, want))
	if err != nil {
		t.Fatalf("parseDiscoverCursor: %v", err)
	}
	if got == nil {
		t.Fatal("got nil cursor")
	}
	if *got != want {
		t.Fatalf("round trip changed the cursor: %+v, want %+v", *got, want)
	}
}

func TestDiscoverCursorPreservesScorePrecisionExactly(t *testing.T) {
	// The keyset comparison includes score equality, so trailing zeros and the
	// full decimal must survive the round trip verbatim.
	for _, score := range []string{"1", "0", "0.5", "0.846800", "0.1234567890"} {
		raw := mustEncode(t, discoverCursor{Version: cursorVersion, Score: score, UserID: uuid.New()})
		got, err := parseDiscoverCursor(raw)
		if err != nil {
			t.Fatalf("score %q: %v", score, err)
		}
		if got.Score != score {
			t.Fatalf("score %q round-tripped as %q", score, got.Score)
		}
	}
}

func TestDiscoverCursorEmptyMeansFirstPage(t *testing.T) {
	got, err := parseDiscoverCursor("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("empty cursor produced %+v, want nil", got)
	}
}

func TestDiscoverCursorIsURLSafe(t *testing.T) {
	raw := mustEncode(t, discoverCursor{Version: cursorVersion, Score: "0.500000", UserID: uuid.New()})
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			t.Fatalf("cursor %q contains %q, which needs URL escaping", raw, r)
		}
	}
}

func TestDiscoverCursorRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"not base64", "not-a-cursor!!"},
		{"base64 but not JSON", base64.RawURLEncoding.EncodeToString([]byte("hello"))},
		{"empty JSON object", base64.RawURLEncoding.EncodeToString([]byte(`{}`))},
		{"JSON array", base64.RawURLEncoding.EncodeToString([]byte(`[1,2,3]`))},
		{"standard base64 padding", base64.StdEncoding.EncodeToString([]byte(`{"v":1,"s":"0.5","u":"x"}`))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDiscoverCursor(tc.raw)
			assertBadRequest(t, err)
			if got != nil {
				t.Fatalf("returned a cursor alongside the error: %+v", got)
			}
		})
	}
}

func TestDiscoverCursorRejectsBadFields(t *testing.T) {
	valid := uuid.New()
	cases := []struct {
		name   string
		cursor discoverCursor
	}{
		{"wrong version", discoverCursor{Version: cursorVersion + 1, Score: "0.5", UserID: valid}},
		{"zero version", discoverCursor{Version: 0, Score: "0.5", UserID: valid}},
		{"nil user id", discoverCursor{Version: cursorVersion, Score: "0.5", UserID: uuid.Nil}},
		{"empty score", discoverCursor{Version: cursorVersion, Score: "", UserID: valid}},
		{"score above one", discoverCursor{Version: cursorVersion, Score: "1.5", UserID: valid}},
		{"negative score", discoverCursor{Version: cursorVersion, Score: "-0.5", UserID: valid}},
		{"non-numeric score", discoverCursor{Version: cursorVersion, Score: "abc", UserID: valid}},
		{"scientific notation", discoverCursor{Version: cursorVersion, Score: "1e-3", UserID: valid}},
		{"too many decimals", discoverCursor{Version: cursorVersion, Score: "0.12345678901", UserID: valid}},
		{"sql fragment", discoverCursor{Version: cursorVersion, Score: "0.5); DROP TABLE users; --", UserID: valid}},
		{"leading plus", discoverCursor{Version: cursorVersion, Score: "+0.5", UserID: valid}},
		{"bare dot", discoverCursor{Version: cursorVersion, Score: "0.", UserID: valid}},
		{"whitespace padded", discoverCursor{Version: cursorVersion, Score: " 0.5 ", UserID: valid}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDiscoverCursor(mustEncode(t, tc.cursor))
			assertBadRequest(t, err)
			if got != nil {
				t.Fatalf("returned a cursor alongside the error: %+v", got)
			}
		})
	}
}

func TestMatchCursorRoundTrip(t *testing.T) {
	// Truncated to microseconds: timestamptz has microsecond resolution, so a
	// finer value could never match a stored row exactly.
	want := matchCursor{
		Version:   cursorVersion,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		ID:        uuid.New(),
	}
	got, err := parseMatchCursor(mustEncode(t, want))
	if err != nil {
		t.Fatalf("parseMatchCursor: %v", err)
	}
	if got.Version != want.Version || got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("round trip changed the cursor: %+v, want %+v", *got, want)
	}
}

func TestMatchCursorEmptyMeansFirstPage(t *testing.T) {
	got, err := parseMatchCursor("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("empty cursor produced %+v, want nil", got)
	}
}

func TestMatchCursorRejectsBadFields(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name   string
		cursor matchCursor
	}{
		{"wrong version", matchCursor{Version: cursorVersion + 1, CreatedAt: now, ID: uuid.New()}},
		{"nil id", matchCursor{Version: cursorVersion, CreatedAt: now, ID: uuid.Nil}},
		{"zero time", matchCursor{Version: cursorVersion, CreatedAt: time.Time{}, ID: uuid.New()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMatchCursor(mustEncode(t, tc.cursor))
			assertBadRequest(t, err)
			if got != nil {
				t.Fatalf("returned a cursor alongside the error: %+v", got)
			}
		})
	}
}

func TestMatchCursorRejectsMalformedInput(t *testing.T) {
	for _, raw := range []string{"!!!", base64.RawURLEncoding.EncodeToString([]byte("nope"))} {
		if _, err := parseMatchCursor(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

func TestCursorsAreNotInterchangeable(t *testing.T) {
	discover := mustEncode(t, discoverCursor{Version: cursorVersion, Score: "0.5", UserID: uuid.New()})
	if _, err := parseMatchCursor(discover); err == nil {
		t.Fatal("a discovery cursor was accepted by the match list")
	}

	match := mustEncode(t, matchCursor{Version: cursorVersion, CreatedAt: time.Now().UTC(), ID: uuid.New()})
	if _, err := parseDiscoverCursor(match); err == nil {
		t.Fatal("a match cursor was accepted by discovery")
	}
}

func TestOrderPairIsCanonicalAndStable(t *testing.T) {
	a := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	b := uuid.MustParse("ffffffff-0000-4000-8000-000000000002")

	first, second := orderPair(a, b)
	if first != a || second != b {
		t.Fatalf("orderPair(a, b) = (%v, %v), want (%v, %v)", first, second, a, b)
	}
	// The matches table enforces user_a_id < user_b_id, so argument order must
	// not change the result.
	reverseFirst, reverseSecond := orderPair(b, a)
	if reverseFirst != first || reverseSecond != second {
		t.Fatalf("orderPair is not order-independent: (%v, %v) vs (%v, %v)",
			first, second, reverseFirst, reverseSecond)
	}

	same, sameToo := orderPair(a, a)
	if same != a || sameToo != a {
		t.Fatalf("orderPair(a, a) = (%v, %v), want (%v, %v)", same, sameToo, a, a)
	}
}

func TestOrderPairMatchesByteOrdering(t *testing.T) {
	// Differences in any byte position must be respected, not just the first.
	base := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	for i := 0; i < len(base); i++ {
		higher := base
		higher[i] = base[i] + 1

		first, second := orderPair(higher, base)
		if first != base || second != higher {
			t.Fatalf("byte %d: orderPair returned (%v, %v), want (%v, %v)", i, first, second, base, higher)
		}
	}
}
