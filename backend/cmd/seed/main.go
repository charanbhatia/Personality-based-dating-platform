package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/joho/godotenv"
)

const seedPassword = "password123"
const numUsers = 55

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
	"Doctor. Caring and committed.",
	"Architect. Design and function.",
	"Writer. Stories and coffee.",
	"Photographer. Capturing moments.",
	"Teacher. Making a difference.",
	"Engineer. Problem solver.",
	"Artist. Painting and sculpture.",
	"Musician. Guitar and vocals.",
	"Chef. Food is art.",
	"Pilot. Sky is the limit.",
	"Lawyer. Justice and balance.",
	"Nurse. Compassion and care.",
	"Designer. UX and aesthetics.",
	"Developer. Code and creativity.",
	"Consultant. Strategy and people.",
	"Researcher. Always asking why.",
	"Entrepreneur. Ideas and execution.",
	"Freelancer. Freedom and flexibility.",
	"Student. Learning and growing.",
	"Retired. Wisdom and travel.",
	"Explorer. Life is an adventure.",
	"Dreamer. Making it happen.",
}

var genders = []string{"Male", "Female", "Non-binary", "Other"}

var locations = []string{
	"Mumbai", "Delhi", "Bangalore", "Hyderabad", "Chennai", "Kolkata", "Pune",
	"Ahmedabad", "Jaipur", "Lucknow", "Chandigarh", "Indore", "Coimbatore",
	"Kochi", "Goa", "Dehradun", "Mysore", "Nagpur", "Bhopal", "Surat",
	"London", "New York", "Sydney", "Toronto", "Dubai", "Singapore",
}

func main() {
	_ = godotenv.Load()
	if err := db.Init(env("DATABASE_URL", "postgres://localhost:5432/dating_platform?sslmode=disable")); err != nil {
		log.Fatalf("database init: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	rand.Seed(time.Now().UnixNano())

	userRepo := repository.NewUserRepo(db.Pool)
	profileRepo := repository.NewProfileRepo(db.Pool)

	for i := 1; i <= numUsers; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		name := firstNames[rand.Intn(len(firstNames))] + " " + lastNames[rand.Intn(len(lastNames))]
		dob := time.Date(1985+rand.Intn(20), time.Month(1+rand.Intn(12)), 1+rand.Intn(28), 0, 0, 0, 0, time.UTC)

		u, err := userRepo.Create(ctx, email, seedPassword, name, &dob)
		if err != nil {
			log.Printf("skip user %s: %v", email, err)
			continue
		}

		bio := bios[rand.Intn(len(bios))]
		gender := genders[rand.Intn(len(genders))]
		location := locations[rand.Intn(len(locations))]

		if err := profileRepo.CreateWith(ctx, u.ID, bio, gender, location, ""); err != nil {
			log.Printf("profile for %s: %v", email, err)
		}

		traits := map[string]float64{
			"openness":          float64(rand.Intn(100)) / 100,
			"conscientiousness": float64(rand.Intn(100)) / 100,
			"extraversion":      float64(rand.Intn(100)) / 100,
			"agreeableness":     float64(rand.Intn(100)) / 100,
			"neuroticism":       float64(rand.Intn(100)) / 100,
		}
		traitsJSON, _ := json.Marshal(traits)
		if err := profileRepo.UpsertPersonality(ctx, u.ID, traitsJSON); err != nil {
			log.Printf("personality for %s: %v", email, err)
		}

		if i%10 == 0 {
			log.Printf("seeded %d users...", i)
		}
	}

	log.Printf("done. seeded %d users. All use password: %s", numUsers, seedPassword)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
