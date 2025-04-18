# go-smtp-server

A full-featured SMTP server implementation in Go that processes, queues, and delivers emails.

## Features

- Simple, lightweight SMTP server written in Go
- Implements core SMTP commands (HELO/EHLO, MAIL FROM, RCPT TO, DATA, QUIT)
- STARTTLS support with automatic self-signed certificate generation
- Email queuing mechanism with persistence to disk
- Actual email delivery with MX record lookup
- Delivery retry with exponential backoff
- Comprehensive test suite for server functionality
- Runs on port 2525 by default

## Usage

### Running the server

```
go run main.go
```

The server will start listening on port 2525 and begin processing emails.

### Using as a library

You can also use this SMTP server as a library in your Go projects:

```go
import "path/to/go-smtp-server"

func main() {
    // Start the server on a specific port (or ":0" for random port)
    stop, addr, err := Start(":2525")
    if err != nil {
        log.Fatal(err)
    }
    
    // addr contains the actual address being used
    log.Printf("SMTP server listening on %s", addr)
    
    // To stop the server later:
    // stop()
}
```

## Email Processing Flow

1. Incoming emails are received through the SMTP protocol
2. Each message is queued to the spool directory as a JSON file
3. A background scheduler processes the queue and attempts delivery:
   - MX record lookup for the recipient domain
   - SMTP connection to the target mail server
   - STARTTLS negotiation if supported
   - Message transmission
4. If delivery fails, the message is kept in the queue with:
   - Error information
   - Attempt counter
   - Next retry time (with exponential backoff)

## Directory Structure

- `protocol.go` - Core SMTP protocol implementation
- `transport.go` - Server setup and TLS configuration
- `deliver.go` - Email queuing and delivery functionality
- `main.go` - Server entry point
- `spool/` - Directory for queued email storage

## Development

This project is a functional SMTP server implementation suitable for development, testing, and production use cases with modest email volume requirements. 