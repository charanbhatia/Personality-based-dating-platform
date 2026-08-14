package realtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/messaging"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/testdb"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const testSecret = "realtime-test-secret"

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

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
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// node is one API replica: its own hub, gateway and HTTP server.
type node struct {
	server *httptest.Server
	hub    *Hub
}

func startNode(t *testing.T, pool *pgxpool.Pool, redis *goredis.Client) *node {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	hub := NewHub(redis, discard())
	ready := make(chan struct{})
	go func() {
		close(ready)
		hub.Run(ctx)
	}()
	<-ready
	// Let the hub establish its Redis subscription before clients join.
	time.Sleep(50 * time.Millisecond)

	svc := messaging.NewService(
		messaging.NewStore(pool),
		messaging.NewMatchGate(pool),
		messaging.Config{MaxMessageLength: 4000},
		nil,
		discard(),
	).WithBroadcaster(NewPublisher(redis, hub))

	gateway := NewServer(hub, svc, nil, Config{
		AllowedOrigins:  []string{"*"},
		MaxMessageBytes: 16 << 10,
		PingInterval:    time.Second,
		PongTimeout:     10 * time.Second,
		WriteTimeout:    5 * time.Second,
		JWTSecret:       testSecret,
	}, discard())

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gateway.Handle)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &node{server: srv, hub: hub}
}

func dial(t *testing.T, n *node, userID uuid.UUID) *websocket.Conn {
	t.Helper()

	token, err := auth.NewJWT(testSecret, userID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	url := "ws" + strings.TrimPrefix(n.server.URL, "http") + "/ws?access_token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial websocket: %v (status %d)", err, status)
	}
	t.Cleanup(func() { _ = conn.Close() })

	expectFrame(t, conn, TypeConnected)
	return conn
}

func send(t *testing.T, conn *websocket.Conn, frame inbound) {
	t.Helper()
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// expectFrame reads until it sees the wanted type or the deadline passes.
func expectFrame(t *testing.T, conn *websocket.Conn, want string) outbound {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var frame outbound
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if frame.Type == want {
			return frame
		}
		if frame.Type == TypeError && want != TypeError {
			t.Fatalf("waiting for %q, got error %s: %s", want, frame.Code, frame.Error)
		}
	}
}

func setup(t *testing.T) (*pgxpool.Pool, *messaging.Service, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	pool := testdb.New(t)
	svc := messaging.NewService(
		messaging.NewStore(pool),
		messaging.NewMatchGate(pool),
		messaging.Config{MaxMessageLength: 4000},
		nil,
		discard(),
	)

	alice := testdb.CreateUser(t, pool, "alice@example.com", "Alice")
	bob := testdb.CreateUser(t, pool, "bob@example.com", "Bob")
	conv, err := svc.OpenByUser(context.Background(), alice, bob)
	if err != nil {
		t.Fatalf("open conversation: %v", err)
	}
	return pool, svc, alice, bob, conv.ID
}

func TestUnauthenticatedUpgradeIsRejected(t *testing.T) {
	pool, _, _, _, _ := setup(t)
	n := startNode(t, pool, testRedis(t))

	url := "ws" + strings.TrimPrefix(n.server.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("expected the handshake to fail without a token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestSubscribeRequiresParticipation(t *testing.T) {
	pool, _, _, _, convID := setup(t)
	mallory := testdb.CreateUser(t, pool, "mallory@example.com", "Mallory")

	n := startNode(t, pool, testRedis(t))
	conn := dial(t, n, mallory)

	send(t, conn, inbound{Type: TypeSubscribe, ConversationID: convID.String()})

	frame := expectFrame(t, conn, TypeError)
	if frame.Code != CodeForbidden {
		t.Errorf("code = %q, want %q", frame.Code, CodeForbidden)
	}
}

func TestSendOverWebSocketReachesTheOtherParticipant(t *testing.T) {
	pool, _, alice, bob, convID := setup(t)
	n := startNode(t, pool, testRedis(t))

	aliceConn := dial(t, n, alice)
	bobConn := dial(t, n, bob)

	for _, c := range []*websocket.Conn{aliceConn, bobConn} {
		send(t, c, inbound{Type: TypeSubscribe, ConversationID: convID.String()})
		expectFrame(t, c, TypeSubscribed)
	}

	send(t, aliceConn, inbound{
		Type:           TypeMessageSend,
		ConversationID: convID.String(),
		Content:        "hello over the socket",
		ClientMsgID:    "ws-1",
	})

	ack := expectFrame(t, aliceConn, TypeMessageAck)
	if ack.ClientMsgID != "ws-1" {
		t.Errorf("ack client_msg_id = %q, want ws-1", ack.ClientMsgID)
	}

	delivered := expectFrame(t, bobConn, TypeMessageNew)
	var msg messaging.Message
	if err := json.Unmarshal(delivered.Message, &msg); err != nil {
		t.Fatalf("decode delivered message: %v", err)
	}
	if msg.Content != "hello over the socket" {
		t.Errorf("content = %q, want the sent text", msg.Content)
	}
	if msg.SenderID != alice {
		t.Errorf("sender = %s, want alice %s", msg.SenderID, alice)
	}
}

// A REST send must reach websocket subscribers, since clients may use either.
func TestRestSendIsDeliveredToSubscribers(t *testing.T) {
	pool, _, alice, bob, convID := setup(t)
	redis := testRedis(t)
	n := startNode(t, pool, redis)

	bobConn := dial(t, n, bob)
	send(t, bobConn, inbound{Type: TypeSubscribe, ConversationID: convID.String()})
	expectFrame(t, bobConn, TypeSubscribed)

	restService := messaging.NewService(
		messaging.NewStore(pool),
		messaging.NewMatchGate(pool),
		messaging.Config{MaxMessageLength: 4000},
		nil,
		discard(),
	).WithBroadcaster(NewPublisher(redis, n.hub))

	if _, _, err := restService.Send(context.Background(), alice, convID, "sent over rest", ""); err != nil {
		t.Fatalf("rest send: %v", err)
	}

	delivered := expectFrame(t, bobConn, TypeMessageNew)
	var msg messaging.Message
	if err := json.Unmarshal(delivered.Message, &msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Content != "sent over rest" {
		t.Errorf("content = %q, want the REST message", msg.Content)
	}
}

// The point of Redis fanout: a client on one replica sees a message sent
// through another.
func TestFanoutReachesClientsOnAnotherReplica(t *testing.T) {
	redis := testRedis(t)
	if redis == nil {
		t.Skip("TEST_REDIS_URL not set; cross-replica fanout needs Redis")
	}

	pool, _, alice, bob, convID := setup(t)
	nodeA := startNode(t, pool, redis)
	nodeB := startNode(t, pool, redis)

	aliceConn := dial(t, nodeA, alice)
	bobConn := dial(t, nodeB, bob)

	for _, c := range []*websocket.Conn{aliceConn, bobConn} {
		send(t, c, inbound{Type: TypeSubscribe, ConversationID: convID.String()})
		expectFrame(t, c, TypeSubscribed)
	}

	send(t, aliceConn, inbound{
		Type:           TypeMessageSend,
		ConversationID: convID.String(),
		Content:        "across replicas",
		ClientMsgID:    "x-1",
	})
	expectFrame(t, aliceConn, TypeMessageAck)

	delivered := expectFrame(t, bobConn, TypeMessageNew)
	var msg messaging.Message
	if err := json.Unmarshal(delivered.Message, &msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Content != "across replicas" {
		t.Errorf("content = %q, want the cross-replica message", msg.Content)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	pool, _, alice, bob, convID := setup(t)
	n := startNode(t, pool, testRedis(t))

	aliceConn := dial(t, n, alice)
	bobConn := dial(t, n, bob)

	send(t, bobConn, inbound{Type: TypeSubscribe, ConversationID: convID.String()})
	expectFrame(t, bobConn, TypeSubscribed)
	send(t, bobConn, inbound{Type: TypeUnsubscribe, ConversationID: convID.String()})

	// Round-trip a ping so the unsubscribe is guaranteed to have been handled.
	send(t, bobConn, inbound{Type: TypePing})
	expectFrame(t, bobConn, TypePong)

	send(t, aliceConn, inbound{
		Type: TypeMessageSend, ConversationID: convID.String(), Content: "anyone there?",
	})

	_ = bobConn.SetReadDeadline(time.Now().Add(time.Second))
	var frame outbound
	if err := bobConn.ReadJSON(&frame); err == nil {
		t.Errorf("received %q after unsubscribing, want nothing", frame.Type)
	}
}

func TestMalformedFramesAreReported(t *testing.T) {
	pool, _, alice, _, _ := setup(t)
	n := startNode(t, pool, testRedis(t))
	conn := dial(t, n, alice)

	if err := conn.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if frame := expectFrame(t, conn, TypeError); frame.Code != CodeBadFrame {
		t.Errorf("code = %q, want %q", frame.Code, CodeBadFrame)
	}

	send(t, conn, inbound{Type: "nonsense"})
	if frame := expectFrame(t, conn, TypeError); frame.Code != CodeBadFrame {
		t.Errorf("code = %q, want %q", frame.Code, CodeBadFrame)
	}

	send(t, conn, inbound{Type: TypeSubscribe, ConversationID: "not-a-uuid"})
	if frame := expectFrame(t, conn, TypeError); frame.Code != CodeInvalid {
		t.Errorf("code = %q, want %q", frame.Code, CodeInvalid)
	}
}

func TestConnectionsAreCounted(t *testing.T) {
	pool, _, alice, _, _ := setup(t)
	n := startNode(t, pool, testRedis(t))

	if got := n.hub.Connections(); got != 0 {
		t.Fatalf("connections = %d before dialling, want 0", got)
	}

	conn := dial(t, n, alice)
	if got := n.hub.Connections(); got != 1 {
		t.Errorf("connections = %d after dialling, want 1", got)
	}

	_ = conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && n.hub.Connections() != 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := n.hub.Connections(); got != 0 {
		t.Errorf("connections = %d after closing, want 0", got)
	}
}
