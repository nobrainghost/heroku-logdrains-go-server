package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/juju/ratelimit"
	_ "github.com/lib/pq"
)

var db *sql.DB

type LogEntry struct {
	ID        int       `json:"id"`
	Source    string    `json:"source"`
	TimeStamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

func initDB() {
	var err error
	connstr := os.Getenv("DATABASE_URL")
	if connstr == "" {
		log.Fatal("DATABASE_URL must be set")
	}

	db, err = sql.Open("postgres", connstr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS logs (
		id SERIAL PRIMARY KEY,
		source TEXT,
		timestamp TIMESTAMP DEFAULT Now(),
		message TEXT
	)`)
	if err != nil {
		log.Fatal("Failed to create table:", err)
	}
}

func saveLog(source, message string) error {
	_, err := db.Exec("INSERT INTO logs (source, message) VALUES ($1, $2)", source, message)
	return err
}

// Middleware: API Authentication
func apiAuthentication() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-KEY")
		expectedKey := os.Getenv("API_KEY")
		if apiKey != expectedKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Middleware Rate Limiting
func rateLimitMiddleware() gin.HandlerFunc {
	bucket := ratelimit.NewBucket(1*time.Second, 5)
	return func(c *gin.Context) {
		if bucket.TakeAvailable(1) == 0 {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Heroku (No Authentication)
func receiveLogs(c *gin.Context) {
	fmt.Println(("Headers:"), c.Request.Header)
	// Ensure the request comes from Heroku Logplex
	userAgent := c.GetHeader("User-Agent")
	if !strings.Contains(userAgent, "Logplex") && !strings.Contains(userAgent, "logfwd") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized source"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading request body"})
		return
	}

	logData := strings.TrimSpace(string(body))
	parts := strings.SplitN(logData, " ", 2)
	if len(parts) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid log entry"})
		return
	}
	source, message := parts[0], parts[1]

	// Save Asynchronously
	go func() {
		err := saveLog(source, message)
		if err != nil {
			fmt.Println("Error saving log entry:", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"status": "Log entry saved"})
}

func getLogs(c *gin.Context) {
	expectedAPIKey := os.Getenv("LOG_API_KEY")
	if expectedAPIKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server misconfiguration: API key missing"})
		return
	}

	apiKey := c.GetHeader("X-API-Key")
	if apiKey != expectedAPIKey {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: Invalid API key"})
		return
	}

	rows, err := db.Query("SELECT id, source, timestamp, message FROM logs ORDER BY timestamp DESC LIMIT 100")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var log LogEntry
		if err := rows.Scan(&log.ID, &log.Source, &log.TimeStamp, &log.Message); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning logs"})
			return
		}
		logs = append(logs, log)
	}

	c.JSON(http.StatusOK, logs)
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	initDB()
	defer db.Close()

	router := gin.Default()

	router.POST("/logs", receiveLogs)

	// Secure Fetch Routes
	authorized := router.Group("/")
	authorized.Use(apiAuthentication(), rateLimitMiddleware())
	authorized.GET("/logs", getLogs)

	router.Run(":8080")
}
