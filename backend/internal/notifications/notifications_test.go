package notifications

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/email"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type capturedMail struct {
	sent []email.Message
}

func (c *capturedMail) Send(_ context.Context, msg email.Message) error {
	c.sent = append(c.sent, msg)
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testRedis returns a client when TEST_REDIS_URL is set, otherwise nil so the
// service exercises its no-cache path.
func testRedis(t *testing.T) *goredis.Client {
	t.Helper()

	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		return nil
	}
	opt, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	client := goredis.NewClient(opt)
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newTestService(t *testing.T) (*Service, *pgxpool.Pool, *capturedMail) {
	t.Helper()

	pool := testdb.New(t)
	mail := &capturedMail{}
	svc := NewService(NewStore(pool), testRedis(t), mail,
		Config{AppBaseURL: "http://localhost:5173"}, discardLogger())
	return svc, pool, mail
}

func TestMatchCreatedNotifiesBothUsers(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	matchID := uuid.New()

	err := svc.OnMatchCreated(ctx, "evt-1", events.MatchCreated{
		MatchID: matchID, UserAID: alice, UserBID: bob, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("handle match: %v", err)
	}

	for _, c := range []struct {
		user       uuid.UUID
		wantInBody string
	}{{alice, "Bob"}, {bob, "Alice"}} {
		items, _, err := svc.List(ctx, c.user, nil, 20)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("user has %d notifications, want 1", len(items))
		}
		if items[0].Type != events.TypeMatchCreated {
			t.Errorf("type = %q, want %q", items[0].Type, events.TypeMatchCreated)
		}
		if !contains(items[0].Body, c.wantInBody) {
			t.Errorf("body = %q, want it to name %q", items[0].Body, c.wantInBody)
		}

		var data map[string]any
		if err := json.Unmarshal(items[0].Data, &data); err != nil {
			t.Fatalf("decode data: %v", err)
		}
		if data["match_id"] != matchID.String() {
			t.Errorf("data.match_id = %v, want %s", data["match_id"], matchID)
		}
	}
}

func TestRedeliveredEventDoesNotDuplicateTheNotification(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	payload := events.MatchCreated{MatchID: uuid.New(), UserAID: alice, UserBID: bob}

	for i := 0; i < 3; i++ {
		if err := svc.OnMatchCreated(ctx, "same-event", payload); err != nil {
			t.Fatalf("handle attempt %d: %v", i, err)
		}
	}

	items, _, err := svc.List(ctx, alice, nil, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("got %d notifications, want 1 for a repeated event_id", len(items))
	}

	count, err := svc.UnreadCount(ctx, alice)
	if err != nil {
		t.Fatalf("unread: %v", err)
	}
	if count != 1 {
		t.Errorf("unread = %d, want 1", count)
	}
}

func TestMessageCreatedNotifiesOnlyTheRecipient(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	sender := testdb.CreateUser(t, pool, "sender@example.com", "Sender")
	recipient := testdb.CreateUser(t, pool, "recipient@example.com", "Recipient")

	err := svc.OnMessageCreated(ctx, "evt-msg", events.MessageCreated{
		ConversationID: uuid.New(),
		MessageID:      uuid.New(),
		SenderID:       sender,
		RecipientID:    recipient,
		Preview:        "are you free on friday?",
	})
	if err != nil {
		t.Fatalf("handle message: %v", err)
	}

	got, _, err := svc.List(ctx, recipient, nil, 20)
	if err != nil {
		t.Fatalf("list recipient: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recipient has %d notifications, want 1", len(got))
	}
	if got[0].Title != "Sender" || got[0].Body != "are you free on friday?" {
		t.Errorf("notification = %q / %q, want the sender name and preview", got[0].Title, got[0].Body)
	}

	senderInbox, _, err := svc.List(ctx, sender, nil, 20)
	if err != nil {
		t.Fatalf("list sender: %v", err)
	}
	if len(senderInbox) != 0 {
		t.Errorf("sender has %d notifications, want 0", len(senderInbox))
	}
}

func TestUnreadCountTracksReadState(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	sender := testdb.CreateUser(t, pool, "sender@example.com", "Sender")

	for i := 0; i < 3; i++ {
		err := svc.OnMessageCreated(ctx, "evt-"+string(rune('a'+i)), events.MessageCreated{
			ConversationID: uuid.New(),
			MessageID:      uuid.New(),
			SenderID:       sender,
			RecipientID:    alice,
			Preview:        "hello",
		})
		if err != nil {
			t.Fatalf("handle %d: %v", i, err)
		}
	}

	assertUnread := func(want int64, when string) {
		t.Helper()
		got, err := svc.UnreadCount(ctx, alice)
		if err != nil {
			t.Fatalf("unread %s: %v", when, err)
		}
		if got != want {
			t.Errorf("unread %s = %d, want %d", when, got, want)
		}
	}

	assertUnread(3, "after three messages")

	items, _, err := svc.List(ctx, alice, nil, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := svc.MarkRead(ctx, alice, items[0].ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	assertUnread(2, "after one read")

	// Reading the same notification again must not double count.
	if err := svc.MarkRead(ctx, alice, items[0].ID); err != nil {
		t.Fatalf("mark read again: %v", err)
	}
	assertUnread(2, "after a repeated read")

	if err := svc.MarkAllRead(ctx, alice); err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	assertUnread(0, "after read-all")
}

func TestMarkReadRejectsAnotherUsersNotification(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	sender := testdb.CreateUser(t, pool, "sender@example.com", "Sender")
	mallory := testdb.CreateUser(t, pool, "mallory@example.com", "Mallory")

	err := svc.OnMessageCreated(ctx, "evt-x", events.MessageCreated{
		ConversationID: uuid.New(), MessageID: uuid.New(),
		SenderID: sender, RecipientID: alice, Preview: "private",
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	items, _, err := svc.List(ctx, alice, nil, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := svc.MarkRead(ctx, mallory, items[0].ID); err != ErrNotFound {
		t.Errorf("mark read as another user = %v, want ErrNotFound", err)
	}

	count, err := svc.UnreadCount(ctx, alice)
	if err != nil {
		t.Fatalf("unread: %v", err)
	}
	if count != 1 {
		t.Errorf("owner unread = %d, want 1: another user must not clear it", count)
	}
}

func TestListPaginatesNewestFirst(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	sender := testdb.CreateUser(t, pool, "sender@example.com", "Sender")

	for i := 0; i < 5; i++ {
		err := svc.OnMessageCreated(ctx, "evt-"+string(rune('a'+i)), events.MessageCreated{
			ConversationID: uuid.New(), MessageID: uuid.New(),
			SenderID: sender, RecipientID: alice, Preview: string(rune('1' + i)),
		})
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	page1, next, err := svc.List(ctx, alice, nil, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 || page1[0].Body != "5" {
		t.Fatalf("page 1 = %v, want the newest first", bodies(page1))
	}
	if next == nil {
		t.Fatal("expected a next cursor")
	}

	k, err := cursor.Decode(*next)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	page2, _, err := svc.List(ctx, alice, &k, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 || page2[0].Body != "3" {
		t.Errorf("page 2 = %v, want 3,2", bodies(page2))
	}
}

func TestPasswordResetSendsMailWithTheToken(t *testing.T) {
	svc, pool, mail := newTestService(t)
	ctx := context.Background()

	user := testdb.CreateUser(t, pool, "reset@example.com", "Reset User")

	err := svc.OnPasswordResetRequested(ctx, events.PasswordResetRequested{
		UserID:     user,
		Email:      "reset@example.com",
		ResetToken: "token-123",
		ExpiresAt:  time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("handle reset: %v", err)
	}

	if len(mail.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(mail.sent))
	}
	if mail.sent[0].To != "reset@example.com" {
		t.Errorf("to = %q, want the user's address", mail.sent[0].To)
	}
	if !contains(mail.sent[0].Body, "token-123") {
		t.Errorf("body %q must carry the reset token", mail.sent[0].Body)
	}
}

func bodies(items []Notification) []string {
	out := make([]string, len(items))
	for i, n := range items {
		out[i] = n.Body
	}
	return out
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
