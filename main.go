package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"database/sql"
	"time"

	_ "github.com/lib/pq"

	"github.com/gin-gonic/gin"
	"github.com/juju/ratelimit"
)

var db *sql.DB

type LogEntry struct {
	ID        int       `json:"id"`
	Source    string    `json:"source"`
	TimeStamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

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

// Rate-Limiting
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

func initDB() {
	var err error
	connstr := os.Getenv(("DATABASE_URL"))
	if connstr == "" {
		log.Fatal("DATABASE_URL must be set")
	}
	db, err = sql.Open("postgres", connstr)
	if err != nil {
		panic(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS logs (
		id SERIAL PRIMARY KEY,
		source TEXT,
		timestamp TIMESTAMP DEFAULT Now(),
		message TEXT
		)`)
	if err != nil {
		panic(err)
	}
}

func saveLog(source, message string) error {
	_, err := db.Exec("INSERT INTO logs (source, message) VALUES ($1, $2)", source, message)
	return err
}

func receiveLogs(c *gin.Context) {
	herokuUserAgent := "Logplex"
	userAgent := c.GetHeader("User-Agent")

	if !strings.Contains(userAgent, herokuUserAgent) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
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

	source := parts[0]
	message := parts[1]

	go func() {
		err := saveLog(source, message)
		if err != nil {
			fmt.Println("Error saving log entry: ", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"status": "Log entry saved"})
}

func getLogs(c *gin.Context) {
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
	router := gin.Default()
	router.SetTrustedProxies([]string{"0.0.0.0"})
	router.Use(rateLimitMiddleware())

	// Public route for Heroku logs
	router.POST("/logs", receiveLogs)

	// Secure routes for fetching logs
	authorized := router.Group("/")
	authorized.Use(apiAuthentication())
	authorized.GET("/logs", getLogs)

	router.Run(":8080")
}
