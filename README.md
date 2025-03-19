# Heroku Logdrains Go Server

Heroku-logdrains-Go-server is a lightweight log ingestion server that receives logs from Heroku log drains and stores them in a PostgreSQL database. It uses Gin for the API framework, supports batch log writing for efficiency, and includes rate limiting and authentication for security.

## Features

- Receives logs from Heroku log drains
- Stores logs in a PostgreSQL database
- Rate limiting to prevent abuse
- Secure log retrieval endpoint

## Installation

### Prerequisites
- Go 1.18+
- PostgreSQL database
- Heroku account (if deploying logs from Heroku)
- Ngrok (if running locally and exposing to Heroku)

### Clone the Repository
```sh
git clone https://github.com/nobrainghost/Heroku-logdrains-go-server.git
cd Heroku-logdrains-go-server
```

### Install Dependencies
```sh
go mod tidy
```

## Configuration
### Set Environment Variables
You need to set the following environment variables:

#### Linux (Bash)
```sh
export DATABASE_URL="postgres://user:password@localhost:5432/dbname"
export API_KEY="your-api-key"
export LOG_API_KEY="your-log-api-key"
```

#### Windows (PowerShell)
```powershell
$env:DATABASE_URL="postgres://user:password@localhost:5432/dbname"
$env:API_KEY="your-api-key"
$env:LOG_API_KEY="your-log-api-key"
```

## Running the Server

### Locally
```sh
go run main.go
```
This starts the server on `http://localhost:8080`

### Exposing the Server for Heroku (Using Ngrok)
If running locally, you need to expose your local server to the internet using Ngrok:

1. Start Ngrok to expose port 8080:
   ```sh
   ngrok http 8080
   ```
2. Copy the generated public URL (`https://your-ngrok-url.ngrok.io`)
3. Set up Heroku log drains (see below) using this URL

## Setting Up Heroku Log Drains

To send logs from Heroku to this server, configure a log drain:

```sh
heroku drains:add https://your-server-url/logs -a your-heroku-app
```
If running locally with Ngrok:
```sh
heroku drains:add https://your-ngrok-url.ngrok.io/logs -a your-heroku-app
```

## API Endpoints

### Receiving Logs (POST `/logs`)
Heroku sends logs via HTTP POST requests to this endpoint.

### Retrieving Logs (GET `/logs`)
Requires authentication using `X-API-KEY`.

Example:
```sh
curl -H "X-API-KEY: your-log-api-key" https://your-server-url/logs
```

## Deployment

To deploy to a server or cloud provider, set the environment variables and run:
```sh
go build -o logserver
./logserver
```

