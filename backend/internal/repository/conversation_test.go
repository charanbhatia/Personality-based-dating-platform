package repository

import (
	"context"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.New(t)
}

func createUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	return testdb.CreateUser(t, pool, email, email)
}

// The list query filters on a single placeholder used twice. Passing the user
// ID twice made pgx reject every call, so the inbox always returned 500.
func TestListByUserIDReturnsConversationsFromBothSides(t *testing.T) {
	pool := testPool(t)
	repo := NewConversationRepo(pool)
	ctx := context.Background()

	alice := createUser(t, pool, "alice@example.com")
	bob := createUser(t, pool, "bob@example.com")
	carol := createUser(t, pool, "carol@example.com")

	if _, err := repo.CreateOrGet(ctx, alice, bob); err != nil {
		t.Fatalf("create alice-bob: %v", err)
	}
	if _, err := repo.CreateOrGet(ctx, bob, carol); err != nil {
		t.Fatalf("create bob-carol: %v", err)
	}

	bobConvs, err := repo.ListByUserID(ctx, bob)
	if err != nil {
		t.Fatalf("list for bob: %v", err)
	}
	if len(bobConvs) != 2 {
		t.Errorf("bob has %d conversations, want 2", len(bobConvs))
	}

	aliceConvs, err := repo.ListByUserID(ctx, alice)
	if err != nil {
		t.Fatalf("list for alice: %v", err)
	}
	if len(aliceConvs) != 1 {
		t.Errorf("alice has %d conversations, want 1", len(aliceConvs))
	}
}

func TestListByUserIDReturnsEmptySliceNotNil(t *testing.T) {
	pool := testPool(t)
	repo := NewConversationRepo(pool)

	convs, err := repo.ListByUserID(context.Background(), createUser(t, pool, "loner@example.com"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if convs == nil {
		t.Fatal("expected an empty slice so the JSON response is [] rather than null")
	}
	if len(convs) != 0 {
		t.Errorf("got %d conversations, want 0", len(convs))
	}
}

// The conversations table requires user1_id < user2_id, so participant order
// must be normalised before insert regardless of caller order.
func TestCreateOrGetIsIdempotentRegardlessOfArgumentOrder(t *testing.T) {
	pool := testPool(t)
	repo := NewConversationRepo(pool)
	ctx := context.Background()

	alice := createUser(t, pool, "alice@example.com")
	bob := createUser(t, pool, "bob@example.com")

	first, err := repo.CreateOrGet(ctx, alice, bob)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := repo.CreateOrGet(ctx, bob, alice)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("got conversations %s and %s, want the same conversation", first.ID, second.ID)
	}
	if first.User1ID.String() >= first.User2ID.String() {
		t.Errorf("participants not normalised: user1=%s user2=%s", first.User1ID, first.User2ID)
	}
}

func TestUserInConversationRejectsNonParticipants(t *testing.T) {
	pool := testPool(t)
	repo := NewConversationRepo(pool)
	ctx := context.Background()

	alice := createUser(t, pool, "alice@example.com")
	bob := createUser(t, pool, "bob@example.com")
	mallory := createUser(t, pool, "mallory@example.com")

	conv, err := repo.CreateOrGet(ctx, alice, bob)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ok, err := repo.UserInConversation(ctx, conv.ID, alice)
	if err != nil || !ok {
		t.Errorf("participant check for alice = (%v, %v), want (true, nil)", ok, err)
	}

	ok, _ = repo.UserInConversation(ctx, conv.ID, mallory)
	if ok {
		t.Error("mallory is not a participant but was allowed into the conversation")
	}
}

func TestMessagesRoundTripInChronologicalOrder(t *testing.T) {
	pool := testPool(t)
	repo := NewConversationRepo(pool)
	ctx := context.Background()

	alice := createUser(t, pool, "alice@example.com")
	bob := createUser(t, pool, "bob@example.com")
	conv, err := repo.CreateOrGet(ctx, alice, bob)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, content := range []string{"hey", "hello", "how are you"} {
		if _, err := repo.SendMessage(ctx, conv.ID, alice, content); err != nil {
			t.Fatalf("send %q: %v", content, err)
		}
	}

	msgs, err := repo.Messages(ctx, conv.ID, 100, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].Content != "hey" || msgs[2].Content != "how are you" {
		t.Errorf("messages out of order: %q ... %q", msgs[0].Content, msgs[2].Content)
	}
}

func TestMessagesReturnsEmptySliceNotNil(t *testing.T) {
	pool := testPool(t)
	repo := NewConversationRepo(pool)
	ctx := context.Background()

	alice := createUser(t, pool, "alice@example.com")
	bob := createUser(t, pool, "bob@example.com")
	conv, err := repo.CreateOrGet(ctx, alice, bob)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	msgs, err := repo.Messages(ctx, conv.ID, 100, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if msgs == nil {
		t.Fatal("expected an empty slice so the JSON response is [] rather than null")
	}
}
