package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/juju/ratelimit"
	_ "github.com/lib/pq"
)

var db *sql.DB

var logChannel = make(chan LogEntry, 100)
var flushInterval = 5 * time.Minute

type LogEntry struct {
	Source    string
	TimeStamp time.Time
	Message   string
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
func batchSaveLogs(logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	// bulk insert query
	query := "INSERT INTO logs (source, timestamp, message) VALUES "
	var args []interface{}
	for i, log := range logs {
		query += fmt.Sprintf("($%d, $%d, $%d),", i*3+1, i*3+2, i*3+3)
		args = append(args, log.Source, log.TimeStamp, log.Message)
	}
	query = strings.TrimSuffix(query, ",")

	_, err := db.Exec(query, args...)
	fmt.Println("Batch insert query:", query)
	return err
}

func flushLogs() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	var logs []LogEntry
	var mu sync.Mutex

	for {
		select {
		case logEntry := <-logChannel:
			mu.Lock()
			logs = append(logs, logEntry)
			mu.Unlock()

		case <-ticker.C:
			mu.Lock()
			if len(logs) > 0 {
				if err := batchSaveLogs(logs); err != nil {
					fmt.Println("Error saving batch logs:", err)
				}
				logs = nil // Clear logs after flush
			}
			mu.Unlock()
		}
	}
}

// func saveLog(source, message string) error {
// 	_, err := db.Exec("INSERT INTO logs (source, message) VALUES ($1, $2)", source, message)
// 	return err
// }

// Heroku (No Authentication)
func receiveLogs(c *gin.Context) {
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

	// Push to channel instead of writing to DB immediately
	logChannel <- LogEntry{
		Source:    source,
		TimeStamp: time.Now(),
		Message:   message,
	}

	c.JSON(http.StatusOK, gin.H{"status": "Log entry received"})
	fmt.Println("Log entry received:")
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
		if err := rows.Scan(&log.Source, &log.TimeStamp, &log.Message); err != nil {
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

	// Start log flushing goroutine
	go flushLogs()

	router := gin.Default()
	router.POST("/logs", receiveLogs)

	// Secure Fetch Routes
	authorized := router.Group("/")
	authorized.Use(apiAuthentication(), rateLimitMiddleware())
	authorized.GET("/logs", getLogs)

	router.Run(":8080")
}
