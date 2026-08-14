package integration

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestSwipeValidation(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	target := onboard(t, onboardOptions{Name: "Target"})

	cases := []struct {
		name       string
		body       map[string]any
		wantStatus int
		wantCode   string
	}{
		{"self", map[string]any{"user_id": viewer.ID, "action": "like"}, http.StatusBadRequest, "cannot_swipe_self"},
		{"unknown action", map[string]any{"user_id": target.ID, "action": "superlike"}, http.StatusBadRequest, "invalid_action"},
		{"empty action", map[string]any{"user_id": target.ID, "action": ""}, http.StatusBadRequest, "invalid_action"},
		// A missing or unusable body field is a field-level validation failure, so
		// the client is told which field and why.
		{"missing user", map[string]any{"action": "like"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"nil user", map[string]any{"user_id": uuid.Nil, "action": "like"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"unknown user", map[string]any{"user_id": uuid.New(), "action": "like"}, http.StatusNotFound, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, http.MethodPost, "/api/v1/likes", viewer.Token, tc.body)
			if resp.Status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", resp.Status, tc.wantStatus, resp.Body)
			}
			if tc.wantCode != "" && resp.Code() != tc.wantCode {
				t.Fatalf("code = %q, want %q", resp.Code(), tc.wantCode)
			}
		})
	}
}

func TestSwipeActionIsCaseAndSpaceInsensitive(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	target := onboard(t, onboardOptions{Name: "Target"})

	result := swipe(t, viewer, target.ID, "  LIKE  ")
	if result.Action != "like" {
		t.Fatalf("action = %q, want the canonical %q", result.Action, "like")
	}
}

func TestSwipeIsIdempotent(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	target := onboard(t, onboardOptions{Name: "Target"})

	first := swipe(t, viewer, target.ID, "pass")
	if first.Duplicate {
		t.Fatal("the first swipe reported duplicate")
	}

	second := swipe(t, viewer, target.ID, "pass")
	if !second.Duplicate {
		t.Fatal("a repeated swipe was not reported as a duplicate")
	}

	// A different action on the same pair must not overwrite the decision, which
	// would let a client undo a pass and re-enter the feed.
	third := swipe(t, viewer, target.ID, "like")
	if !third.Duplicate || third.Action != "pass" {
		t.Fatalf("re-swiping returned %+v, want the original pass reported as a duplicate", third)
	}
	if got := countRows(t, `SELECT count(*) FROM swipes WHERE from_user_id = $1`, viewer.ID); got != 1 {
		t.Fatalf("swipes rows = %d, want 1", got)
	}
}

func TestMutualLikeCreatesExactlyOneMatch(t *testing.T) {
	resetDB(t)
	a := onboard(t, onboardOptions{Name: "Alpha"})
	b := onboard(t, onboardOptions{Name: "Bravo"})

	first := swipe(t, a, b.ID, "like")
	if first.Matched {
		t.Fatal("a one-sided like produced a match")
	}
	if first.MatchID != nil {
		t.Fatal("match_id is set on an unmatched like")
	}

	second := swipe(t, b, a.ID, "like")
	if !second.Matched || second.MatchID == nil {
		t.Fatalf("the reciprocal like did not match: %+v", second)
	}

	if got := countRows(t, `SELECT count(*) FROM matches`); got != 1 {
		t.Fatalf("matches rows = %d, want 1", got)
	}
	// The canonical ordering the table enforces.
	var userA, userB uuid.UUID
	err := testPool.QueryRow(context.Background(),
		`SELECT user_a_id, user_b_id FROM matches`).Scan(&userA, &userB)
	if err != nil {
		t.Fatalf("load match: %v", err)
	}
	if userA.String() >= userB.String() {
		t.Fatalf("match stored as (%v, %v), want ascending order", userA, userB)
	}

	// Exactly one event, so the notification worker cannot double-send.
	if got := countRows(t, `SELECT count(*) FROM outbox_events WHERE event_type = 'match.created'`); got != 1 {
		t.Fatalf("match.created events = %d, want 1", got)
	}

	// Both sides see the same match.
	for _, u := range []*user{a, b} {
		page := listMatches(t, u, "")
		if len(page.Items) != 1 {
			t.Fatalf("%s sees %d matches, want 1", u.Name, len(page.Items))
		}
		if page.Items[0].MatchID != *second.MatchID {
			t.Errorf("%s sees match %v, want %v", u.Name, page.Items[0].MatchID, *second.MatchID)
		}
		if !page.Items[0].User.IsMatched {
			t.Errorf("%s sees is_matched=false on their own match", u.Name)
		}
		if page.Items[0].CompatibilityScore == nil {
			t.Errorf("%s sees a null compatibility score", u.Name)
		}
	}

	// Replaying the like must report the existing match rather than creating one.
	replay := swipe(t, b, a.ID, "like")
	if !replay.Duplicate || !replay.Matched || *replay.MatchID != *second.MatchID {
		t.Fatalf("replay returned %+v, want the existing match", replay)
	}
	if got := countRows(t, `SELECT count(*) FROM matches`); got != 1 {
		t.Fatalf("matches rows = %d after a replay, want 1", got)
	}
}

func TestMatchStoresTheSymmetricScore(t *testing.T) {
	resetDB(t)
	a := onboard(t, onboardOptions{Name: "Alpha", Level: LevelLowest})
	b := onboard(t, onboardOptions{Name: "Bravo", Level: LevelHighest})

	swipe(t, a, b.ID, "like")
	swipe(t, b, a.ID, "like")

	// Opposite ends of every trait: unweighted similarity is 0.
	var score *float64
	err := testPool.QueryRow(context.Background(),
		`SELECT compatibility_score FROM matches`).Scan(&score)
	if err != nil {
		t.Fatalf("load score: %v", err)
	}
	if score == nil {
		t.Fatal("compatibility_score was not recorded")
	}
	if *score != 0 {
		t.Fatalf("compatibility_score = %v, want 0", *score)
	}

	// Both participants must be told the same number, whatever their own weights.
	setPreferences(t, a, map[string]any{
		"age_min": 18, "age_max": 99,
		"genders":       []string{"man", "woman", "nonbinary", "other"},
		"trait_weights": map[string]float64{"openness": 5},
	})
	aScore := listMatches(t, a, "").Items[0].CompatibilityScore
	bScore := listMatches(t, b, "").Items[0].CompatibilityScore
	if aScore == nil || bScore == nil || *aScore != *bScore {
		t.Fatalf("participants see different scores: %v vs %v", aScore, bScore)
	}
}

func TestConcurrentMutualLikesProduceOneMatch(t *testing.T) {
	resetDB(t)
	// Under READ COMMITTED neither transaction would see the other's uncommitted
	// like, so without the pair advisory lock this can leave a reciprocal pair
	// with no match row at all.
	for attempt := 0; attempt < 8; attempt++ {
		a := onboard(t, onboardOptions{Name: fmt.Sprintf("Alpha %d", attempt)})
		b := onboard(t, onboardOptions{Name: fmt.Sprintf("Bravo %d", attempt)})

		start := make(chan struct{})
		results := make([]swipeResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			results[0] = swipe(t, a, b.ID, "like")
		}()
		go func() {
			defer wg.Done()
			<-start
			results[1] = swipe(t, b, a.ID, "like")
		}()
		close(start)
		wg.Wait()

		matches := countRows(t,
			`SELECT count(*) FROM matches WHERE user_a_id IN ($1, $2) AND user_b_id IN ($1, $2)`, a.ID, b.ID)
		if matches != 1 {
			t.Fatalf("attempt %d: matches = %d, want exactly 1 (results: %+v)", attempt, matches, results)
		}

		// Whichever transaction committed second must report the match, and at
		// least one caller must learn about it.
		if !results[0].Matched && !results[1].Matched {
			t.Fatalf("attempt %d: neither caller was told about the match: %+v", attempt, results)
		}

		events := countRows(t, `
			SELECT count(*) FROM outbox_events
			WHERE event_type = 'match.created'
			  AND payload->>'user_a_id' IN ($1, $2)
			  AND payload->>'user_b_id' IN ($1, $2)`, a.ID.String(), b.ID.String())
		if events != 1 {
			t.Fatalf("attempt %d: match.created events = %d, want exactly 1", attempt, events)
		}
	}
}

func TestConcurrentDuplicateLikesRecordOneSwipe(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	target := onboard(t, onboardOptions{Name: "Target"})

	const parallel = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			swipe(t, viewer, target.ID, "like")
		}()
	}
	close(start)
	wg.Wait()

	if got := countRows(t, `SELECT count(*) FROM swipes WHERE from_user_id = $1`, viewer.ID); got != 1 {
		t.Fatalf("swipes rows = %d, want 1", got)
	}
}

func TestMatchListPagination(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})

	const total = 7
	expected := make(map[uuid.UUID]bool, total)
	for i := 0; i < total; i++ {
		other := onboard(t, onboardOptions{Name: fmt.Sprintf("Match %02d", i)})
		swipe(t, viewer, other.ID, "like")
		swipe(t, other, viewer.ID, "like")
		expected[other.ID] = true
	}

	seen := make(map[uuid.UUID]int, total)
	cursor := ""
	var previous string
	for pages := 0; ; pages++ {
		if pages > total+2 {
			t.Fatal("pagination did not terminate")
		}
		query := "limit=3"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		page := listMatches(t, viewer, query)
		for _, item := range page.Items {
			seen[item.User.UserID]++
			// Newest first.
			if previous != "" && item.CreatedAt > previous {
				t.Fatalf("match created at %s follows %s; the list is not newest-first",
					item.CreatedAt, previous)
			}
			previous = item.CreatedAt
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("walked %d distinct matches, want %d", len(seen), total)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("match with %v appeared %d times", id, count)
		}
		if !expected[id] {
			t.Errorf("unexpected counterparty %v", id)
		}
	}
}

func TestMatchListRejectsBadParameters(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	for _, query := range []string{"limit=0", "limit=9999", "cursor=nope"} {
		do(t, http.MethodGet, "/api/v1/matches?"+query, viewer.Token, nil).
			requireStatus(t, http.StatusBadRequest)
	}
}

func TestBlockHidesTheRelationshipBothWays(t *testing.T) {
	resetDB(t)
	blocker := onboard(t, onboardOptions{Name: "Blocker"})
	blocked := onboard(t, onboardOptions{Name: "Blocked"})

	swipe(t, blocker, blocked.ID, "like")
	swipe(t, blocked, blocker.ID, "like")
	if len(listMatches(t, blocker, "").Items) != 1 {
		t.Fatal("expected a match before the block")
	}

	do(t, http.MethodPost, "/api/v1/blocks", blocker.Token,
		map[string]any{"user_id": blocked.ID}).requireStatus(t, http.StatusNoContent)

	// The match row survives, but neither side sees it.
	if got := countRows(t, `SELECT count(*) FROM matches`); got != 1 {
		t.Errorf("the block deleted the match row (%d rows); it should only hide it", got)
	}
	if got := len(listMatches(t, blocker, "").Items); got != 0 {
		t.Errorf("the blocker still sees %d matches", got)
	}
	if got := len(listMatches(t, blocked, "").Items); got != 0 {
		t.Errorf("the blocked user still sees %d matches", got)
	}

	// The blocker is told plainly; the blocked user gets the same answer as for a
	// non-existent account, so the block itself is not disclosed.
	do(t, http.MethodPost, "/api/v1/likes", blocker.Token,
		map[string]any{"user_id": blocked.ID, "action": "like"}).
		requireError(t, http.StatusConflict, "blocked_by_you")
	do(t, http.MethodPost, "/api/v1/likes", blocked.Token,
		map[string]any{"user_id": blocker.ID, "action": "like"}).
		requireError(t, http.StatusNotFound, "not_found")

	do(t, http.MethodGet, "/api/v1/users/"+blocker.ID.String()+"/public", blocked.Token, nil).
		requireStatus(t, http.StatusNotFound)
	do(t, http.MethodGet, "/api/v1/users/"+blocked.ID.String()+"/public", blocker.Token, nil).
		requireStatus(t, http.StatusNotFound)

	// One event for the messaging domain to close the conversation on.
	if got := countRows(t, `SELECT count(*) FROM outbox_events WHERE event_type = 'user.blocked'`); got != 1 {
		t.Errorf("user.blocked events = %d, want 1", got)
	}
}

func TestBlockRemovesBothSidesFromDiscovery(t *testing.T) {
	resetDB(t)
	blocker := onboard(t, onboardOptions{Name: "Blocker"})
	blocked := onboard(t, onboardOptions{Name: "Blocked"})

	if !discover(t, blocker, "").contains(blocked.ID) {
		t.Fatal("setup: the candidate is not discoverable")
	}
	do(t, http.MethodPost, "/api/v1/blocks", blocker.Token,
		map[string]any{"user_id": blocked.ID}).requireStatus(t, http.StatusNoContent)

	if discover(t, blocker, "").contains(blocked.ID) {
		t.Error("the blocked user still appears in the blocker's feed")
	}
	if discover(t, blocked, "").contains(blocker.ID) {
		t.Error("the blocker still appears in the blocked user's feed")
	}
}

func TestBlockIsIdempotentAndReversible(t *testing.T) {
	resetDB(t)
	blocker := onboard(t, onboardOptions{Name: "Blocker"})
	blocked := onboard(t, onboardOptions{Name: "Blocked"})
	swipe(t, blocker, blocked.ID, "like")
	swipe(t, blocked, blocker.ID, "like")

	for i := 0; i < 3; i++ {
		do(t, http.MethodPost, "/api/v1/blocks", blocker.Token,
			map[string]any{"user_id": blocked.ID}).requireStatus(t, http.StatusNoContent)
	}
	if got := countRows(t, `SELECT count(*) FROM blocks`); got != 1 {
		t.Fatalf("blocks rows = %d, want 1", got)
	}
	// Only the first block is an event.
	if got := countRows(t, `SELECT count(*) FROM outbox_events WHERE event_type = 'user.blocked'`); got != 1 {
		t.Fatalf("user.blocked events = %d, want 1", got)
	}

	for i := 0; i < 3; i++ {
		do(t, http.MethodDelete, "/api/v1/blocks/"+blocked.ID.String(), blocker.Token, nil).
			requireStatus(t, http.StatusNoContent)
	}
	if got := countRows(t, `SELECT count(*) FROM blocks`); got != 0 {
		t.Fatalf("blocks rows = %d after unblocking, want 0", got)
	}
	if got := len(listMatches(t, blocker, "").Items); got != 1 {
		t.Fatalf("the match did not reappear after unblocking: %d items", got)
	}
}

func TestBlockValidation(t *testing.T) {
	resetDB(t)
	u := onboard(t, onboardOptions{Name: "Viewer"})

	do(t, http.MethodPost, "/api/v1/blocks", u.Token, map[string]any{"user_id": u.ID}).
		requireError(t, http.StatusBadRequest, "cannot_block_self")
	do(t, http.MethodPost, "/api/v1/blocks", u.Token, map[string]any{"user_id": uuid.New()}).
		requireStatus(t, http.StatusNotFound)
	do(t, http.MethodPost, "/api/v1/blocks", u.Token, map[string]any{}).
		requireError(t, http.StatusUnprocessableEntity, "validation_failed")
	// A path parameter is rejected before validation, as a malformed request.
	do(t, http.MethodDelete, "/api/v1/blocks/not-a-uuid", u.Token, nil).
		requireStatus(t, http.StatusBadRequest)
}

func TestBlockListIsScopedToTheCaller(t *testing.T) {
	resetDB(t)
	blocker := onboard(t, onboardOptions{Name: "Blocker"})
	other := onboard(t, onboardOptions{Name: "Other Blocker"})
	first := onboard(t, onboardOptions{Name: "First Blocked"})
	second := onboard(t, onboardOptions{Name: "Second Blocked"})

	for _, target := range []*user{first, second} {
		do(t, http.MethodPost, "/api/v1/blocks", blocker.Token,
			map[string]any{"user_id": target.ID}).requireStatus(t, http.StatusNoContent)
	}
	do(t, http.MethodPost, "/api/v1/blocks", other.Token,
		map[string]any{"user_id": first.ID}).requireStatus(t, http.StatusNoContent)

	var list struct {
		Items []struct {
			UserID uuid.UUID `json:"user_id"`
			Name   string    `json:"name"`
		} `json:"items"`
	}
	do(t, http.MethodGet, "/api/v1/blocks", blocker.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &list)

	if len(list.Items) != 2 {
		t.Fatalf("listed %d blocks, want 2", len(list.Items))
	}
	// Newest first.
	if list.Items[0].UserID != second.ID {
		t.Errorf("first entry is %v, want the most recent block %v", list.Items[0].UserID, second.ID)
	}
}

func TestReport(t *testing.T) {
	resetDB(t)
	reporter := onboard(t, onboardOptions{Name: "Reporter"})
	reported := onboard(t, onboardOptions{Name: "Reported"})

	t.Run("rejects self", func(t *testing.T) {
		do(t, http.MethodPost, "/api/v1/reports", reporter.Token,
			map[string]any{"user_id": reporter.ID, "reason": "spam"}).
			requireError(t, http.StatusBadRequest, "cannot_report_self")
	})

	t.Run("rejects an unknown reason", func(t *testing.T) {
		resp := do(t, http.MethodPost, "/api/v1/reports", reporter.Token,
			map[string]any{"user_id": reported.ID, "reason": "because"}).
			requireStatus(t, http.StatusUnprocessableEntity)
		if _, ok := resp.Details()["reason"]; !ok {
			t.Fatalf("no detail for the reason field: %s", resp.Body)
		}
	})

	t.Run("rejects overlong details", func(t *testing.T) {
		long := make([]rune, 1001)
		for i := range long {
			long[i] = 'a'
		}
		do(t, http.MethodPost, "/api/v1/reports", reporter.Token, map[string]any{
			"user_id": reported.ID, "reason": "spam", "details": string(long),
		}).requireStatus(t, http.StatusUnprocessableEntity)
	})

	t.Run("rejects an unknown user", func(t *testing.T) {
		do(t, http.MethodPost, "/api/v1/reports", reporter.Token,
			map[string]any{"user_id": uuid.New(), "reason": "spam"}).
			requireStatus(t, http.StatusNotFound)
	})

	t.Run("accepts and then collapses duplicates", func(t *testing.T) {
		type reportResult struct {
			ID        uuid.UUID `json:"id"`
			Duplicate bool      `json:"duplicate"`
		}
		var first reportResult
		do(t, http.MethodPost, "/api/v1/reports", reporter.Token, map[string]any{
			"user_id": reported.ID, "reason": "harassment", "details": "first report",
		}).requireStatus(t, http.StatusAccepted).decode(t, &first)
		if first.Duplicate {
			t.Fatal("the first report was reported as a duplicate")
		}

		var second reportResult
		do(t, http.MethodPost, "/api/v1/reports", reporter.Token, map[string]any{
			"user_id": reported.ID, "reason": "spam", "details": "second report",
		}).requireStatus(t, http.StatusAccepted).decode(t, &second)
		if !second.Duplicate || second.ID != first.ID {
			t.Fatalf("a second open report returned %+v, want the existing %v", second, first.ID)
		}
		if got := countRows(t, `SELECT count(*) FROM reports`); got != 1 {
			t.Fatalf("reports rows = %d, want 1 while a case is open", got)
		}
	})

	t.Run("does not block or unmatch on its own", func(t *testing.T) {
		// Reporting is a moderation signal; hiding the user is a separate,
		// explicit action.
		if got := countRows(t, `SELECT count(*) FROM blocks`); got != 0 {
			t.Fatalf("reporting created %d blocks", got)
		}
	})
}

func TestIsMatchedHelper(t *testing.T) {
	resetDB(t)
	a := onboard(t, onboardOptions{Name: "Alpha"})
	b := onboard(t, onboardOptions{Name: "Bravo"})
	c := onboard(t, onboardOptions{Name: "Charlie"})
	ctx := context.Background()

	// This is the seam Person C's messaging domain gates conversations on.
	assertMatched := func(x, y uuid.UUID, want bool, label string) {
		t.Helper()
		got, err := testApp.Matching.IsMatched(ctx, x, y)
		if err != nil {
			t.Fatalf("%s: IsMatched: %v", label, err)
		}
		if got != want {
			t.Errorf("%s: IsMatched = %v, want %v", label, got, want)
		}
	}

	assertMatched(a.ID, b.ID, false, "before any swipe")
	swipe(t, a, b.ID, "like")
	assertMatched(a.ID, b.ID, false, "after a one-sided like")
	swipe(t, b, a.ID, "like")

	assertMatched(a.ID, b.ID, true, "after the reciprocal like")
	assertMatched(b.ID, a.ID, true, "with the arguments reversed")
	assertMatched(a.ID, c.ID, false, "an unrelated pair")
	assertMatched(a.ID, a.ID, false, "a user against themselves")
	assertMatched(a.ID, uuid.New(), false, "an unknown user")
}

func TestPublicProfileReflectsMatchState(t *testing.T) {
	resetDB(t)
	a := onboard(t, onboardOptions{Name: "Alpha"})
	b := onboard(t, onboardOptions{Name: "Bravo"})

	type publicProfile struct {
		UserID    uuid.UUID `json:"user_id"`
		Name      string    `json:"name"`
		Email     string    `json:"email"`
		IsMatched bool      `json:"is_matched"`
	}

	fetch := func() publicProfile {
		var out publicProfile
		do(t, http.MethodGet, "/api/v1/users/"+b.ID.String()+"/public", a.Token, nil).
			requireStatus(t, http.StatusOK).decode(t, &out)
		return out
	}

	before := fetch()
	if before.IsMatched {
		t.Error("is_matched is true before matching")
	}
	if before.Email != "" {
		t.Errorf("the public profile exposes an email: %q", before.Email)
	}
	if before.Name != b.Name {
		t.Errorf("name = %q, want %q", before.Name, b.Name)
	}

	swipe(t, a, b.ID, "like")
	swipe(t, b, a.ID, "like")

	if !fetch().IsMatched {
		t.Error("is_matched is still false after matching")
	}
}

func TestPublicProfileOfUnknownUser(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})

	do(t, http.MethodGet, "/api/v1/users/"+uuid.NewString()+"/public", viewer.Token, nil).
		requireStatus(t, http.StatusNotFound)
	do(t, http.MethodGet, "/api/v1/users/not-a-uuid/public", viewer.Token, nil).
		requireStatus(t, http.StatusBadRequest)

	// Requesting your own id through the public route must work rather than 404.
	do(t, http.MethodGet, "/api/v1/users/"+viewer.ID.String()+"/public", viewer.Token, nil).
		requireStatus(t, http.StatusOK)
}
