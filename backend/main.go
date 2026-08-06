package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"receipt-reconciliation/handlers"
	"receipt-reconciliation/jobs"
	"receipt-reconciliation/metrics"
	"receipt-reconciliation/middleware"
	"receipt-reconciliation/repository"
	"receipt-reconciliation/services"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using default values")
	}

	// Get database configuration from environment
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbName := getEnv("DB_NAME", "receipt_reconciliation")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgres")
	jwtSecret := resolveJWTSecret()

	// Connect to database
	db, err := repository.NewDatabase(dbHost, dbPort, dbName, dbUser, dbPassword)
	if err != nil {
		log.Printf("Database unavailable, continuing in demo mode: %v", err)
		db = nil
	}
	if db != nil {
		defer db.Close()
	}

	// Initialize job queue only if database is available
	var jobQueue *jobs.JobQueue
	if db != nil {
		ocrService := services.NewOCRService()
		companyAPI := services.NewCompanyAPIClient()
		jobQueue = jobs.NewJobQueue(db, 3, ocrService, companyAPI) // 3 worker goroutines

		// Register job handlers (placeholder implementations)
		jobQueue.RegisterHandler(jobs.JobOCRProcess, func(job *jobs.Job, db *repository.Database, ocrService services.OCRService, companyAPI *services.CompanyAPIClient) error {
			log.Printf("Processing OCR job %d", job.ID)
			// TODO: Implement actual OCR processing
			return nil
		})

		jobQueue.RegisterHandler(jobs.JobReconciliation, func(job *jobs.Job, db *repository.Database, ocrService services.OCRService, companyAPI *services.CompanyAPIClient) error {
			log.Printf("Processing reconciliation job %d", job.ID)
			// TODO: Implement actual reconciliation
			return nil
		})

		jobQueue.RegisterHandler(jobs.JobNotification, func(job *jobs.Job, db *repository.Database, ocrService services.OCRService, companyAPI *services.CompanyAPIClient) error {
			log.Printf("Processing notification job %d", job.ID)
			// TODO: Implement actual notification
			return nil
		})

		// Start job queue in background
		go jobQueue.Start()
		defer jobQueue.Stop()

		// Expose queue depth so a backlog is visible before it becomes an outage.
		queue := jobQueue
		metrics.RegisterGauge("job_queue_pending", "Jobs waiting to be claimed.", func() float64 {
			count, err := queue.PendingCount()
			if err != nil {
				return -1
			}
			return float64(count)
		})
		metrics.RegisterGauge("job_queue_workers", "Configured job queue workers.", func() float64 {
			return float64(3)
		})
	}

	// Create handlers
	authHandler := handlers.NewAuthHandler(db, jwtSecret)
	receiptHandler := handlers.NewReceiptHandler(jobQueue)
	invoiceHandler := handlers.NewInvoiceHandler(db)
	historyHandler := handlers.NewHistoryHandler(db)
	dashboardHandler := handlers.NewDashboardHandler(db)

	// Get auth middleware
	authMiddleware := authHandler.GetAuthMiddleware()

	// Create router
	router := mux.NewRouter()

	// Apply middleware
	router.Use(middleware.CORS)
	router.Use(middleware.Logging)

	// Public endpoints (no authentication required)
	router.HandleFunc("/auth/login", authHandler.Login).Methods("POST")
	router.HandleFunc("/auth/register", authHandler.Register).Methods("POST")
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}).Methods("GET")

	// Prometheus scrape target. monitoring/prometheus.yml points at this path,
	// which did not exist before, so the backend target was always down.
	router.Handle("/metrics", metrics.Handler()).Methods("GET")

	// Protected endpoints (authentication required)
	protectedRouter := router.PathPrefix("").Subrouter()
	protectedRouter.Use(authMiddleware.Authenticate)

	// Receipt endpoints (protected)
	financeRouter := protectedRouter.PathPrefix("").Subrouter()
	financeRouter.Use(authMiddleware.RequireRole("FINANCE_STAFF"))
	financeRouter.HandleFunc("/receipts/upload", receiptHandler.UploadReceipt).Methods("POST")
	financeRouter.HandleFunc("/ocr/process", receiptHandler.ProcessOCR).Methods("POST")
	financeRouter.HandleFunc("/reconcile", receiptHandler.Reconcile).Methods("POST")

	// Invoice endpoints (protected)
	financeRouter.HandleFunc("/invoices", invoiceHandler.GetInvoice).Methods("GET")
	financeRouter.HandleFunc("/payments", invoiceHandler.GetPayments).Methods("GET")

	// History endpoints (protected)
	financeRouter.HandleFunc("/history", historyHandler.GetReconciliationHistory).Methods("GET")
	financeRouter.HandleFunc("/audit-logs", historyHandler.GetAuditLogs).Methods("GET")
	financeRouter.HandleFunc("/dashboard", dashboardHandler.GetDashboard).Methods("GET")
	financeRouter.HandleFunc("/reviews/decision", receiptHandler.ReviewDecision).Methods("POST")

	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./frontend")))

	// Start server. Explicit timeouts stop a slow or idle client from holding a
	// connection open indefinitely; WriteTimeout allows for the synchronous OCR
	// path, which waits on the engine and then on the company API.
	port := getEnv("PORT", "8080")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Starting server on port %s", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Shut down on SIGINT/SIGTERM instead of being killed mid-request. log.Fatal
	// exited the process immediately, so deferred cleanup never ran and in-flight
	// jobs were left claimed in the database.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Printf("Server error: %v", err)
	case sig := <-shutdown:
		log.Printf("Received %s, shutting down gracefully...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Graceful shutdown timed out, closing remaining connections: %v", err)
			server.Close()
		}
	}

	// Deferred jobQueue.Stop() and db.Close() run from here, draining workers
	// before the process exits.
	log.Println("Server stopped")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// minJWTSecretLength is the shortest signing key accepted. HS256 keys shorter
// than the 256-bit output are brute-forceable offline from a single token.
const minJWTSecretLength = 32

// weakJWTSecrets are placeholder values shipped in this repository and its docs.
// They are public knowledge, so a token signed with one can be forged by anyone.
var weakJWTSecrets = map[string]bool{
	"your-secret-key-change-in-production": true,
	"change-me-in-production":              true,
	"changeme":                             true,
	"secret":                               true,
	"jwt-secret":                           true,
	"test-secret":                          true,
}

// resolveJWTSecret loads the JWT signing key, refusing to fall back to a known
// value. Previously an unset JWT_SECRET silently selected a hardcoded default,
// which let anyone mint a valid ADMIN token for any deployment.
func resolveJWTSecret() string {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	production := handlers.IsProduction()

	if secret == "" {
		if production {
			log.Fatal("JWT_SECRET must be set when APP_ENV=production")
		}

		generated, err := middleware.GenerateRandomSecret()
		if err != nil {
			log.Fatalf("Failed to generate a JWT signing key: %v", err)
		}
		log.Println("WARNING: JWT_SECRET is not set. Generated a random key for this process;")
		log.Println("         existing tokens are invalid and all sessions end when it restarts.")
		return generated
	}

	if weakJWTSecrets[strings.ToLower(secret)] {
		if production {
			log.Fatal("JWT_SECRET is set to a well-known placeholder value; set a unique secret")
		}
		log.Println("WARNING: JWT_SECRET is a well-known placeholder value. Anyone can forge tokens.")
		log.Println("         Set a unique secret before deploying (see backend/.env.example).")
		return secret
	}

	if len(secret) < minJWTSecretLength {
		if production {
			log.Fatalf("JWT_SECRET must be at least %d characters when APP_ENV=production", minJWTSecretLength)
		}
		log.Printf("WARNING: JWT_SECRET is shorter than %d characters and is weak against offline attacks.", minJWTSecretLength)
	}

	return secret
}
