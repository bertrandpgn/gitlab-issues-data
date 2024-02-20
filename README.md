# Gitlab issues data

Get total spent time for gitlab issues.

Usage: 

```bash
cp .env.dist .env 

# fill .env with correct values

go run main.go
```

Build:

```bash
GOOS=darwin GOARCH=amd64 go build main.go # intel macOS
GOOS=darwin GOARCH=arm64 go build main.go # mX macOS
```