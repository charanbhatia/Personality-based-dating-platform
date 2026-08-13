package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const matchesDDL = `CREATE TABLE IF NOT EXISTS matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_a_id, user_b_id),
    CHECK (user_a_id < user_b_id)
)`

// recordingPublisher captures emitted events so tests can assert the contract
// other domains consume.
type recordingPublisher struct {
	mu       sync.Mutex
	streams  []string
	types    []string
	payloads []any
}

func (p *recordingPublisher) Publish(_ context.Context, stream, eventType string, payload any) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streams = append(p.streams, stream)
	p.types = append(p.types, eventType)
	p.payloads = append(p.payloads, payload)
	return "event-1", nil
}

func newService(pool *pgxpool.Pool, gateEnabled bool) *Service {
	return newServiceWithPublisher(pool, gateEnabled, nil)
}

func newServiceWithPublisher(pool *pgxpool.Pool, gateEnabled bool, pub Publisher) *Service {
	return NewService(NewStore(pool), NewMatchGate(pool), Config{
		MaxMessageLength: 4000,
		MatchGateEnabled: gateEnabled,
	}, pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// dropMatches removes Person B's table so the gate's "not shipped yet"
// behaviour can be exercised deterministically.
func dropMatches(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS matches CASCADE"); err != nil {
		t.Fatalf("drop matches: %v", err)
	}
}

// createMatch simulates Person B's matches table landing.
func createMatch(t *testing.T, pool *pgxpool.Pool, a, b uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, matchesDDL); err != nil {
		t.Fatalf("create matches table: %v", err)
	}
	if a.String() > b.String() {
		a, b = b, a
	}
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO matches (user_a_id, user_b_id) VALUES ($1, $2) RETURNING id`, a, b).Scan(&id)
	if err != nil {
		t.Fatalf("insert match: %v", err)
	}
	t.Cleanup(func() { dropMatches(t, pool) })
	return id
}

func TestOpenByMatchRequiresTheCallerToBeAParticipant(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, true)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	mallory := testdb.CreateUser(t, pool, "mallory@example.com", "Mallory")
	matchID := createMatch(t, pool, alice, bob)

	conv, err := svc.OpenByMatch(ctx, alice, matchID)
	if err != nil {
		t.Fatalf("alice opening her own match: %v", err)
	}
	if conv.Peer.UserID != bob {
		t.Errorf("peer = %s, want bob %s", conv.Peer.UserID, bob)
	}
	if conv.MatchID == nil || *conv.MatchID != matchID {
		t.Error("conversation should record the match it was opened from")
	}

	if _, err := svc.OpenByMatch(ctx, mallory, matchID); !errors.Is(err, ErrForbidden) {
		t.Errorf("mallory opening someone else's match = %v, want ErrForbidden", err)
	}
}

func TestOpenByMatchIsIdempotent(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, true)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	matchID := createMatch(t, pool, alice, bob)

	first, err := svc.OpenByMatch(ctx, alice, matchID)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	second, err := svc.OpenByMatch(ctx, bob, matchID)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("got %s and %s, want the same conversation", first.ID, second.ID)
	}
}

func TestOpenByMatchReportsWhenMatchesTableIsMissing(t *testing.T) {
	pool := testdb.New(t)
	dropMatches(t, pool)
	svc := newService(pool, true)

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")

	_, err := svc.OpenByMatch(context.Background(), alice, uuid.New())
	if !errors.Is(err, ErrMatchTableMissing) {
		t.Errorf("err = %v, want ErrMatchTableMissing so the API can answer 503", err)
	}
}

func TestOpenByUserIsRejectedWhileTheMatchGateIsOn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")

	if _, err := newService(pool, true).OpenByUser(ctx, alice, bob); !errors.Is(err, ErrGateRequired) {
		t.Errorf("err = %v, want ErrGateRequired", err)
	}

	conv, err := newService(pool, false).OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open with the gate disabled: %v", err)
	}
	if conv.Peer.UserID != bob {
		t.Errorf("peer = %s, want bob", conv.Peer.UserID)
	}
}

func TestBlockedUsersCannotOpenOrUseConversations(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, false)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")

	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Person B's blocks table, created here to verify the gate reads it.
	_, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS blocks (
	    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	    PRIMARY KEY (blocker_id, blocked_id))`)
	if err != nil {
		t.Fatalf("create blocks: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS blocks CASCADE")
	})

	if _, err := pool.Exec(ctx, `INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, bob, alice); err != nil {
		t.Fatalf("insert block: %v", err)
	}

	if _, _, err := svc.Send(ctx, alice, conv.ID, "still there?", ""); !errors.Is(err, ErrBlocked) {
		t.Errorf("send after block = %v, want ErrBlocked", err)
	}
}

func TestSendIsIdempotentOnClientMsgID(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, false)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	first, created, err := svc.Send(ctx, alice, conv.ID, "hello", "client-1")
	if err != nil || !created {
		t.Fatalf("first send: created=%v err=%v", created, err)
	}

	second, created, err := svc.Send(ctx, alice, conv.ID, "hello", "client-1")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if created {
		t.Error("retry with the same client_msg_id must not create a second message")
	}
	if first.ID != second.ID {
		t.Errorf("retry returned %s, want the original %s", second.ID, first.ID)
	}

	msgs, _, err := svc.ListMessages(ctx, alice, conv.ID, nil, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("stored %d messages, want 1", len(msgs))
	}
}

func TestSendValidatesContent(t *testing.T) {
	pool := testdb.New(t)
	svc := NewService(NewStore(pool), NewMatchGate(pool), Config{MaxMessageLength: 10},
		nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, _, err := svc.Send(ctx, alice, conv.ID, "   ", ""); !errors.Is(err, ErrEmptyContent) {
		t.Errorf("blank content = %v, want ErrEmptyContent", err)
	}
	if _, _, err := svc.Send(ctx, alice, conv.ID, "way past the limit", ""); !errors.Is(err, ErrContentTooLong) {
		t.Errorf("oversized content = %v, want ErrContentTooLong", err)
	}
}

func TestNonParticipantCannotReadOrWrite(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, false)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	mallory := testdb.CreateUser(t, pool, "mallory@example.com", "Mallory")

	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, _, err := svc.ListMessages(ctx, mallory, conv.ID, nil, 50); !errors.Is(err, ErrForbidden) {
		t.Errorf("read = %v, want ErrForbidden", err)
	}
	if _, _, err := svc.Send(ctx, mallory, conv.ID, "hi", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("send = %v, want ErrForbidden", err)
	}
	if err := svc.MarkRead(ctx, mallory, conv.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("read receipt = %v, want ErrForbidden", err)
	}
}

func TestMessagePagesWalkBackwardsInAscendingOrder(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, false)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, body := range []string{"m1", "m2", "m3", "m4", "m5"} {
		if _, _, err := svc.Send(ctx, alice, conv.ID, body, ""); err != nil {
			t.Fatalf("send %s: %v", body, err)
		}
	}

	page1, next, err := svc.ListMessages(ctx, alice, conv.ID, nil, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 || page1[0].Content != "m4" || page1[1].Content != "m5" {
		t.Fatalf("page 1 = %v, want the newest two in ascending order", contents(page1))
	}
	if next == nil {
		t.Fatal("expected a cursor for older messages")
	}

	k, err := cursor.Decode(*next)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	page2, _, err := svc.ListMessages(ctx, alice, conv.ID, &k, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 || page2[0].Content != "m2" || page2[1].Content != "m3" {
		t.Errorf("page 2 = %v, want m2,m3", contents(page2))
	}
}

func TestInboxReportsUnreadCountsAndHidesEmail(t *testing.T) {
	pool := testdb.New(t)
	svc := newService(pool, false)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, body := range []string{"one", "two"} {
		if _, _, err := svc.Send(ctx, bob, conv.ID, body, ""); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	inbox, _, err := svc.ListConversations(ctx, alice, nil, 20)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("inbox has %d items, want 1", len(inbox))
	}
	if inbox[0].UnreadCount != 2 {
		t.Errorf("unread = %d, want 2", inbox[0].UnreadCount)
	}
	if inbox[0].Peer.Name != "Bob" {
		t.Errorf("peer name = %q, want Bob", inbox[0].Peer.Name)
	}
	if inbox[0].LastMessagePreview != "two" {
		t.Errorf("preview = %q, want the latest message", inbox[0].LastMessagePreview)
	}

	// The sender has nothing unread of their own.
	senderInbox, _, err := svc.ListConversations(ctx, bob, nil, 20)
	if err != nil {
		t.Fatalf("sender inbox: %v", err)
	}
	if senderInbox[0].UnreadCount != 0 {
		t.Errorf("sender unread = %d, want 0", senderInbox[0].UnreadCount)
	}

	if err := svc.MarkRead(ctx, alice, conv.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	afterRead, _, err := svc.ListConversations(ctx, alice, nil, 20)
	if err != nil {
		t.Fatalf("inbox after read: %v", err)
	}
	if afterRead[0].UnreadCount != 0 {
		t.Errorf("unread after mark-read = %d, want 0", afterRead[0].UnreadCount)
	}

	encoded, err := json.Marshal(afterRead[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "@example.com") {
		t.Errorf("conversation payload leaks an email address: %s", encoded)
	}
}

func TestSendPublishesMessageCreatedOnceToTheRecipient(t *testing.T) {
	pool := testdb.New(t)
	pub := &recordingPublisher{}
	svc := newServiceWithPublisher(pool, false, pub)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	conv, err := svc.OpenByUser(ctx, alice, bob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, _, err := svc.Send(ctx, alice, conv.ID, "ping", "dedupe-1"); err != nil {
		t.Fatalf("send: %v", err)
	}
	// A replayed send must not publish a second event.
	if _, _, err := svc.Send(ctx, alice, conv.ID, "ping", "dedupe-1"); err != nil {
		t.Fatalf("resend: %v", err)
	}

	if len(pub.types) != 1 {
		t.Fatalf("published %d events, want 1: %v", len(pub.types), pub.types)
	}
	if pub.types[0] != events.TypeMessageCreated {
		t.Errorf("event type = %q, want %q", pub.types[0], events.TypeMessageCreated)
	}
	if pub.streams[0] != events.StreamNotifications {
		t.Errorf("stream = %q, want %q", pub.streams[0], events.StreamNotifications)
	}

	payload, ok := pub.payloads[0].(events.MessageCreated)
	if !ok {
		t.Fatalf("payload type = %T, want events.MessageCreated", pub.payloads[0])
	}
	if payload.SenderID != alice || payload.RecipientID != bob {
		t.Errorf("sender=%s recipient=%s, want sender=%s recipient=%s",
			payload.SenderID, payload.RecipientID, alice, bob)
	}
	if payload.Preview != "ping" {
		t.Errorf("preview = %q, want %q", payload.Preview, "ping")
	}
}

func contents(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}
