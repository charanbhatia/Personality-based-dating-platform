package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool connects to TEST_DATABASE_URL and applies the schema. Tests that
// need a database are skipped when the variable is unset so the default
// `go test ./...` run stays dependency-free.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(pool.Close)

	schema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "001_init.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func createUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash, name) VALUES ($1, $2, 'x', $2)`, id, email)
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return id
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
