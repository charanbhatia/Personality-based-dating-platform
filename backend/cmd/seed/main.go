// Command seed populates the database with demo users.
//
// Every seeded account is fully onboarded — profile, complete Big Five vector and
// preferences — because a user missing any of those is not discoverable, which
// made the previous seed data produce an empty feed.
//
//	go run ./cmd/seed              # 55 demo users + 10 matched conversations
//	go run ./cmd/seed -users 200   # different volume
//	go run ./cmd/seed -matches 0   # users only
//	go run ./cmd/seed -reset       # delete seeded users first
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/logging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

const (
	seedPassword     = "password123"
	seedEmailPattern = "user%d@example.com"
	// seedEmailDomain scopes -reset so it can never touch a real account.
	seedEmailDomain = "@example.com"
	// randomSeed keeps runs reproducible: the same -users count always yields the
	// same profiles, which makes manual testing and screenshots stable.
	randomSeed = 20240501
)

var firstNames = []string{
	"Alex", "Jordan", "Sam", "Taylor", "Morgan", "Riley", "Casey", "Avery",
	"Jamie", "Quinn", "Reese", "Sage", "Dakota", "Skyler", "Parker", "Cameron",
	"Charlie", "Finley", "Blake", "Hayden", "Emery", "Rowan", "Peyton", "River",
	"Arjun", "Priya", "Vikram", "Ananya", "Rohan", "Ishita", "Karan", "Neha",
	"Aditya", "Sneha", "Rahul", "Kavya", "Dev", "Meera", "Aarav", "Diya",
	"Liam", "Emma", "Noah", "Olivia", "Ethan", "Ava", "Mason", "Sophia",
	"James", "Isabella", "Benjamin", "Mia", "Lucas", "Charlotte",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis",
	"Rodriguez", "Martinez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore",
	"Jackson", "Martin", "Lee", "Thompson", "White", "Harris", "Clark", "Lewis",
	"Robinson", "Walker", "Young", "Hall", "Allen", "King", "Wright", "Scott",
	"Green", "Baker", "Adams", "Nelson", "Carter", "Mitchell", "Perez", "Roberts",
	"Turner", "Phillips", "Campbell", "Parker", "Evans", "Edwards", "Collins",
	"Singh", "Sharma", "Patel", "Kumar", "Reddy", "Nair", "Menon",
}

var bios = []string{
	"Love hiking and coffee. Looking for something real.",
	"Book lover and weekend chef. Let's chat!",
	"Adventure seeker. Travel enthusiast. Dog person.",
	"Software engineer by day, musician by night.",
	"Yoga and mindfulness. Seeking a genuine connection.",
	"Foodie who loves trying new restaurants.",
	"Into fitness and healthy living. Say hi!",
	"Art and music lover. Looking for my person.",
	"Travel addict. 20 countries and counting.",
	"Quiet evenings and good conversations.",
	"Outdoorsy. Camping, trails, and stargazing.",
	"Creative soul. Writer and photographer.",
	"Music festivals and live gigs. Let's go!",
	"Family-oriented. Values matter.",
	"Fitness coach. Let's get strong together.",
	"Wine enthusiast. Food and travel.",
	"Minimalist. Into deep conversations.",
	"Startup life. Looking for a partner in crime.",
	"Teacher. Kids and books. Coffee required.",
	"Nature lover. Sustainability matters.",
	"Movie buff. Pop culture nerd.",
	"Runner. Marathoner. Early bird.",
	"Homebody who loves game nights.",
	"Fashion and design. Creative mind.",
	"Science nerd. Always learning.",
	"Volunteer. Community first.",
	"Pet parent. Two cats and a dog.",
	"Beach person. Sun, sand, and books.",
	"Gamer and tech enthusiast.",
	"Meditation and growth mindset.",
	"Cooking is my love language.",
	"History and museums. Curious soul.",
	"Dance and movement. Life is rhythm.",
	"Entrepreneur. Building something new.",
	"Architect. Design and function.",
	"Photographer. Capturing moments.",
	"Engineer. Problem solver.",
	"Musician. Guitar and vocals.",
	"Chef. Food is art.",
	"Designer. UX and aesthetics.",
	"Researcher. Always asking why.",
	"Freelancer. Freedom and flexibility.",
	"Explorer. Life is an adventure.",
}

var interestPool = []string{
	"hiking", "coffee", "live music", "cooking", "photography", "running",
	"yoga", "board games", "travel", "reading", "cycling", "painting",
	"films", "gardening", "climbing", "swimming", "podcasts", "baking",
	"dogs", "cats", "chess", "surfing", "languages", "volunteering",
}

var locations = []string{
	"Mumbai", "Delhi", "Bangalore", "Hyderabad", "Chennai", "Kolkata", "Pune",
	"Ahmedabad", "Jaipur", "Lucknow", "Chandigarh", "Indore", "Coimbatore",
	"Kochi", "Goa", "Dehradun", "Mysore", "Nagpur", "Bhopal", "Surat",
	"London", "New York", "Sydney", "Toronto", "Dubai", "Singapore",
}

func main() {
	users := flag.Int("users", 55, "number of demo users to create")
	reset := flag.Bool("reset", false, "delete existing seeded users before inserting")
	pairs := flag.Int("matches", 10, "number of matched pairs (and conversations) to create among the first 2N users")
	flag.Parse()

	_ = godotenv.Load()
	logging.Setup(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))

	if err := run(*users, *reset, *pairs); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run(userCount int, reset bool, pairCount int) error {
	if userCount < 1 {
		return fmt.Errorf("users must be at least 1")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, db.Options{MaxConns: 4})
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := ensureSchema(ctx, pool); err != nil {
		return err
	}

	if reset {
		deleted, err := deleteSeeded(ctx, pool)
		if err != nil {
			return err
		}
		slog.Info("seeded users deleted", "count", deleted)
	}

	// One bcrypt hash reused across accounts: they all share a password, and
	// hashing per user would dominate the runtime for no benefit.
	passwordHash, err := auth.HashPassword(seedPassword)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}

	rng := rand.New(rand.NewSource(randomSeed))
	created, skipped := 0, 0

	for i := 1; i <= userCount; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		person := buildPerson(rng, i)
		inserted, err := insertPerson(ctx, pool, person, passwordHash)
		if err != nil {
			return fmt.Errorf("seed %s: %w", person.Email, err)
		}
		if inserted {
			created++
		} else {
			skipped++
		}
		if i%25 == 0 {
			slog.Info("seeding", "processed", i, "of", userCount)
		}
	}

	slog.Info("seed complete",
		"created", created,
		"already_present", skipped,
		"password", seedPassword,
		"example_login", fmt.Sprintf(seedEmailPattern, 1),
	)

	if pairCount > 0 {
		matched, convos, err := seedMatches(ctx, pool, userCount, pairCount)
		if err != nil {
			return err
		}
		slog.Info("seeded matches",
			"pairs", matched,
			"conversations", convos,
			"try", fmt.Sprintf("%s / %s (password %s)", fmt.Sprintf(seedEmailPattern, 1), fmt.Sprintf(seedEmailPattern, 2), seedPassword),
		)
	}
	return nil
}

// ensureSchema fails early with an actionable message when migrations have not
// been applied, instead of dying on a missing column mid-insert.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var hasColumn bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'profiles' AND column_name = 'photo_urls'
		)`).Scan(&hasColumn)
	if err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	if !hasColumn {
		return fmt.Errorf("schema is out of date; run: go run ./cmd/migrate")
	}
	return nil
}

func deleteSeeded(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	// Restricted to the seed domain and to the generated address pattern so a
	// mistyped flag cannot remove real accounts. Profiles, preferences, scores,
	// swipes and matches all cascade from users.
	tag, err := pool.Exec(ctx,
		`DELETE FROM users WHERE email LIKE 'user%' || $1`, seedEmailDomain)
	if err != nil {
		return 0, fmt.Errorf("delete seeded users: %w", err)
	}
	return tag.RowsAffected(), nil
}

type person struct {
	Email       string
	Name        string
	DateOfBirth time.Time
	Bio         string
	Gender      domain.Gender
	Location    string
	Interests   []string
	Traits      domain.Traits
	AgeMin      int
	AgeMax      int
	SeekGenders []string
	PhotoURL    string
}

func buildPerson(rng *rand.Rand, index int) person {
	genders := domain.Genders()
	gender := genders[rng.Intn(len(genders))]

	age := 21 + rng.Intn(24)
	dob := time.Date(time.Now().UTC().Year()-age, time.Month(1+rng.Intn(12)), 1+rng.Intn(28), 0, 0, 0, 0, time.UTC)

	interestCount := 3 + rng.Intn(4)
	interests := make([]string, 0, interestCount)
	used := make(map[string]struct{}, interestCount)
	for len(interests) < interestCount {
		candidate := interestPool[rng.Intn(len(interestPool))]
		if _, dup := used[candidate]; dup {
			continue
		}
		used[candidate] = struct{}{}
		interests = append(interests, candidate)
	}

	traits := domain.Traits{}
	for _, key := range domain.TraitKeys() {
		// Two decimal places keeps the JSON readable while still producing a wide
		// spread of compatibility scores.
		traits[key] = float64(rng.Intn(101)) / 100
	}

	// Seek at least one gender, biased towards two so mutual discoverability is
	// common enough to demo the match flow.
	seeking := domain.GenderStrings()
	rng.Shuffle(len(seeking), func(i, j int) { seeking[i], seeking[j] = seeking[j], seeking[i] })
	seeking = seeking[:1+rng.Intn(2)]

	ageMin := age - 5
	if ageMin < auth.MinimumAge {
		ageMin = auth.MinimumAge
	}

	return person{
		Email:       fmt.Sprintf(seedEmailPattern, index),
		Name:        firstNames[rng.Intn(len(firstNames))] + " " + lastNames[rng.Intn(len(lastNames))],
		DateOfBirth: dob,
		Bio:         bios[rng.Intn(len(bios))],
		Gender:      gender,
		Location:    locations[rng.Intn(len(locations))],
		Interests:   interests,
		Traits:      traits,
		AgeMin:      ageMin,
		AgeMax:      age + 8,
		SeekGenders: seeking,
		// Deterministic placeholder avatars keep the UI populated without bundling
		// image assets.
		PhotoURL: fmt.Sprintf("https://i.pravatar.cc/512?u=%s", fmt.Sprintf(seedEmailPattern, index)),
	}
}

// insertPerson creates one fully onboarded account, reporting false when the
// email already exists so the seed is safe to re-run.
func insertPerson(ctx context.Context, pool *pgxpool.Pool, p person, passwordHash string) (bool, error) {
	inserted := false
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, name, date_of_birth)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (email) DO NOTHING
			RETURNING id`, p.Email, passwordHash, p.Name, p.DateOfBirth).Scan(&userID)
		if err != nil {
			if db.IsNoRows(err) {
				return nil
			}
			return fmt.Errorf("insert user: %w", err)
		}
		inserted = true

		photoURLs, err := json.Marshal([]string{p.PhotoURL})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO profiles (user_id, bio, gender, location, interests, photo_urls, primary_photo_url, photo_url)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::text, $8::varchar)`,
			userID, p.Bio, string(p.Gender), p.Location, p.Interests, photoURLs, p.PhotoURL, p.PhotoURL); err != nil {
			return fmt.Errorf("insert profile: %w", err)
		}

		traits, err := json.Marshal(p.Traits)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO personality_scores (user_id, traits, version, assessed_at)
			VALUES ($1, $2::jsonb, $3, now())`,
			userID, traits, domain.TraitsVersion); err != nil {
			return fmt.Errorf("insert personality scores: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO preferences (user_id, age_min, age_max, genders)
			VALUES ($1, $2, $3, $4)`,
			userID, p.AgeMin, p.AgeMax, p.SeekGenders); err != nil {
			return fmt.Errorf("insert preferences: %w", err)
		}
		return nil
	})
	return inserted, err
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&ok)
	return ok, err
}

func lookupSeededUser(ctx context.Context, pool *pgxpool.Pool, index int) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, fmt.Sprintf(seedEmailPattern, index)).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup %s: %w", fmt.Sprintf(seedEmailPattern, index), err)
	}
	return id, nil
}

func orderPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

// seedMatches creates reciprocal likes, match rows and a conversation with one
// message for the first 2N seeded users so the messaging and match APIs have
// something to show without a manual swipe session.
func seedMatches(ctx context.Context, pool *pgxpool.Pool, userCount, pairCount int) (int, int, error) {
	hasMatches, err := tableExists(ctx, pool, "matches")
	if err != nil {
		return 0, 0, err
	}
	if !hasMatches {
		slog.Warn("matches table missing; skip -matches (run migrations first)")
		return 0, 0, nil
	}

	maxPairs := userCount / 2
	if pairCount > maxPairs {
		pairCount = maxPairs
	}

	hasConversations, err := tableExists(ctx, pool, "conversations")
	if err != nil {
		return 0, 0, err
	}

	matched, convos := 0, 0
	for i := 0; i < pairCount; i++ {
		a, err := lookupSeededUser(ctx, pool, 2*i+1)
		if err != nil {
			return matched, convos, err
		}
		b, err := lookupSeededUser(ctx, pool, 2*i+2)
		if err != nil {
			return matched, convos, err
		}

		if _, err := pool.Exec(ctx, `
			INSERT INTO swipes (from_user_id, to_user_id, action)
			VALUES ($1, $2, 'like'), ($2, $1, 'like')
			ON CONFLICT (from_user_id, to_user_id) DO NOTHING`, a, b); err != nil {
			return matched, convos, fmt.Errorf("seed swipes: %w", err)
		}

		first, second := orderPair(a, b)
		var matchID uuid.UUID
		err = pool.QueryRow(ctx, `
			INSERT INTO matches (user_a_id, user_b_id, compatibility_score)
			VALUES ($1, $2, 0.82)
			ON CONFLICT (user_a_id, user_b_id) DO UPDATE SET compatibility_score = matches.compatibility_score
			RETURNING id`, first, second).Scan(&matchID)
		if err != nil {
			return matched, convos, fmt.Errorf("seed match: %w", err)
		}
		matched++

		if !hasConversations {
			continue
		}

		var convID uuid.UUID
		err = pool.QueryRow(ctx, `
			INSERT INTO conversations (user1_id, user2_id, match_id, last_message_at, last_message_preview)
			VALUES ($1, $2, $3, now(), $4)
			ON CONFLICT (user1_id, user2_id) DO UPDATE SET match_id = COALESCE(conversations.match_id, EXCLUDED.match_id)
			RETURNING id`, first, second, matchID, "Hey — matched on Kindred.").Scan(&convID)
		if err != nil {
			// Older conversations tables have no unique (user1,user2) or match_id.
			err = pool.QueryRow(ctx, `
				SELECT id FROM conversations
				WHERE (user1_id = $1 AND user2_id = $2) OR (user1_id = $2 AND user2_id = $1)
				LIMIT 1`, first, second).Scan(&convID)
			if err != nil {
				err = pool.QueryRow(ctx, `
					INSERT INTO conversations (user1_id, user2_id)
					VALUES ($1, $2) RETURNING id`, first, second).Scan(&convID)
			}
			if err != nil {
				return matched, convos, fmt.Errorf("seed conversation: %w", err)
			}
		}
		convos++

		if _, err := pool.Exec(ctx, `
			INSERT INTO messages (conversation_id, sender_id, content)
			SELECT $1, $2, $3
			WHERE NOT EXISTS (SELECT 1 FROM messages WHERE conversation_id = $1)`,
			convID, a, "Hey — matched on Kindred. Want to grab coffee?"); err != nil {
			return matched, convos, fmt.Errorf("seed message: %w", err)
		}
	}
	return matched, convos, nil
}

