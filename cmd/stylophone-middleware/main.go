package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stylophone/server"
)

func main() {
	addr := flag.String("addr", ":8090", "HTTP address for websocket middleware")
	interval := flag.Duration("poll-interval", 15*time.Millisecond, "touchpad poll interval")
	initialOctave := flag.Int("octave", 4, "initial octave")
	minOctave := flag.Int("min-octave", 0, "minimum octave")
	maxOctave := flag.Int("max-octave", 8, "maximum octave")
	disableInputLock := flag.Bool("disable-input-lock", false, "disable cursor/gesture lock while running")
	disableGestures := flag.Bool("disable-gestures-suppress", false, "do not disable system trackpad gestures in defaults")
	flag.Parse()

	if err := loadDotEnv(".env"); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL, err := mustEnv("DATABASE_URL")
	if err != nil {
		log.Fatal(err)
	}
	googleClientID, err := mustEnv("GOOGLE_OAUTH_CLIENT_ID")
	if err != nil {
		log.Fatal(err)
	}
	jwtSecret, err := mustEnv("AUTH_JWT_SECRET")
	if err != nil {
		log.Fatal(err)
	}
	accessTokenTTL, err := durationFromEnv("AUTH_ACCESS_TOKEN_TTL", 30*time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	challengeTTL, err := durationFromEnv("AUTH_LOGIN_CHALLENGE_TTL", 5*time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	jwtIssuer := os.Getenv("AUTH_JWT_ISSUER")

	middleware, err := server.NewWebSocketMiddleware(server.Config{
		Address:          *addr,
		PollInterval:     *interval,
		InitialOctave:    *initialOctave,
		MinOctave:        *minOctave,
		MaxOctave:        *maxOctave,
		DisableInputLock: *disableInputLock,
		DisableGestures:  *disableGestures,
		DatabaseURL:      databaseURL,
		GoogleClientID:   googleClientID,
		JWTSecret:        jwtSecret,
		JWTIssuer:        jwtIssuer,
		AccessTokenTTL:   accessTokenTTL,
		ChallengeTTL:     challengeTTL,
	})
	if err != nil {
		log.Fatalf("Не удалось создать middleware: %v", err)
	}

	log.Printf("Stylophone middleware запущен на %s", *addr)
	log.Println("WebSocket endpoint: /ws")
	log.Println("Auth REST API: /api/auth/config, /api/auth/nickname-availability, /api/auth/register, /api/auth/login/start, /api/auth/login/complete, /api/auth/me")
	log.Println("События: stylophone:event, stylophone:octave, stylophone:status, drumpad:event")

	err = middleware.Run(ctx)
	if err != nil && !errors.Is(err, server.ErrEscapePressed) {
		log.Fatalf("Middleware завершился с ошибкой: %v", err)
	}

	if errors.Is(err, server.ErrEscapePressed) {
		log.Println("Остановка по Esc")
		return
	}

	log.Println("Остановка завершена")
}
