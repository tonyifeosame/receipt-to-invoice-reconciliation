package main

import (
	"log"
	"net/http"
	"os"
	"receipt-reconciliation/handlers"
	"receipt-reconciliation/jobs"
	"receipt-reconciliation/middleware"
	"receipt-reconciliation/repository"

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
	jwtSecret := getEnv("JWT_SECRET", "your-secret-key-change-in-production")

	// Connect to database
	db, err := repository.NewDatabase(dbHost, dbPort, dbName, dbUser, dbPassword)
	if err != nil {
		log.Printf("Database unavailable, continuing in demo mode: %v", err)
		db = nil
	}
	if db != nil {
		defer db.Close()
	}

	// Create handlers
	authHandler := handlers.NewAuthHandler(db, jwtSecret)
	receiptHandler := handlers.NewReceiptHandler(db)
	invoiceHandler := handlers.NewInvoiceHandler(db)
	historyHandler := handlers.NewHistoryHandler(db)
	dashboardHandler := handlers.NewDashboardHandler(db)

	// Get auth middleware
	authMiddleware := authHandler.GetAuthMiddleware()

	// Initialize job queue only if database is available
	var jobQueue *jobs.JobQueue
	if db != nil {
		jobQueue = jobs.NewJobQueue(db, 3) // 3 worker goroutines

		// Register job handlers (placeholder implementations)
		jobQueue.RegisterHandler(jobs.JobOCRProcess, func(job *jobs.Job, db *repository.Database) error {
			log.Printf("Processing OCR job %d", job.ID)
			// TODO: Implement actual OCR processing
			return nil
		})

		jobQueue.RegisterHandler(jobs.JobReconciliation, func(job *jobs.Job, db *repository.Database) error {
			log.Printf("Processing reconciliation job %d", job.ID)
			// TODO: Implement actual reconciliation
			return nil
		})

		jobQueue.RegisterHandler(jobs.JobNotification, func(job *jobs.Job, db *repository.Database) error {
			log.Printf("Processing notification job %d", job.ID)
			// TODO: Implement actual notification
			return nil
		})

		// Start job queue in background
		go jobQueue.Start()
		defer jobQueue.Stop()
	}

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

	// Start server
	port := getEnv("PORT", "8080")
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
