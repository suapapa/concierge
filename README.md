# Concierge

![concierge logo](./_img/logo.png)

A web application for temporarily hosting files publicly. Files are automatically deleted after a specified time-to-live (TTL) period, making it perfect for sharing files temporarily.

## Features

- **Temporary file hosting**: Upload files and get a shareable key
- **Automatic cleanup**: Files are automatically deleted after TTL expires
- **Active reference tracking**: Files won't be deleted while they're being downloaded
- **Configurable TTL**: Set custom expiration time per file
- **MIME type support**: Preserve or override file MIME types
- **Size limit**: Configurable file size limit (default: 5MB)

## Installation

### Build from source

```sh
go build -o concierge
```

### Docker

```sh
docker build -t concierge .
```

## Usage

### Start the server

```sh
# Default settings (port 8080, temp dir /tmp/concierge, 5MB limit)
./concierge

# Custom configuration
./concierge -p 9000 -t /tmp/myfiles -l 10485760  # 10MB limit
```

**Command-line flags:**
- `-p`: Port number (default: 8080)
- `-t`: Temporary directory path (default: /tmp/concierge)
- `-l`: File size limit in bytes (default: 5242880 = 5MB)

### Upload a file

```sh
# Basic upload (default TTL: 3 minutes)
curl -X POST http://localhost:8080/luggage \
  -F "file=@example.txt"

# Upload with custom MIME type and TTL
curl -X POST http://localhost:8080/luggage \
  -F "file=@example.txt" \
  -F "mime=text/plain" \
  -F "ttl=5"

# Upload an image with 10 minute TTL
curl -X POST http://localhost:8080/luggage \
  -F "file=@image.png" \
  -F "mime=image/png" \
  -F "ttl=10"
```

**Response:**
```json
{
  "key": "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6"
}
```

### Fetch a file

```sh
# Download using the key from upload response
curl http://localhost:8080/luggage/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6 -o downloaded.txt

# Or open in browser
open http://localhost:8080/luggage/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
```

### Complete example

```sh
# 1. Start the server
./concierge

# 2. Upload a file
KEY=$(curl -s -X POST http://localhost:8080/luggage \
  -F "file=@document.pdf" \
  -F "mime=application/pdf" \
  -F "ttl=15" | jq -r '.key')

echo "File uploaded! Key: $KEY"

# 3. Share the URL
echo "Share this URL: http://localhost:8080/luggage/$KEY"

# 4. Download the file
curl http://localhost:8080/luggage/$KEY -o downloaded.pdf
```

## How it works

1. When you upload a file via `/luggage` (POST), it generates a unique key and stores the file in a temporary directory
2. The file metadata (MIME type, filename) is stored in `info.yaml` alongside the file
3. A background goroutine waits for the TTL period, then checks if there are any active downloads
4. If no active references exist, the file is deleted. If downloads are in progress, deletion is delayed until all downloads complete
5. Active reference counting ensures files aren't deleted while being served

## Documentation maintenance

Code changes that affect behavior, flags, APIs, or how the project is built or run should be reflected **immediately** in **`README.md`** (this file) and **`AGENTS.md`** (guidance for coding agents). Update both in the same change whenever practical so they do not drift from the codebase.

## Notes

- Files are stored in the temporary directory specified by the `-t` flag
- Each file gets its own directory named after its key
- The server uses file locking to handle concurrent access in multi-instance deployments
- Default TTL is 3 minutes if not specified
- Maximum file size is 5MB by default (configurable via `-l` flag)
