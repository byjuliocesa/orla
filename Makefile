# The binary with the web client embedded. `go build ./cmd/orla` alone builds it without.
.PHONY: all web web-dev test clean

all: web
	go build -tags webui -o orla ./cmd/orla

# The client is exported by Next.js, then copied where go:embed can reach it.
web:
	cd web && npm ci --no-audit --no-fund && npm run build
	rm -rf internal/webui/dist
	cp -r web/out internal/webui/dist

web-dev:
	cd web && npm run dev

test:
	go test -race ./...
	cd web && npm test

clean:
	rm -rf orla web/out web/.next internal/webui/dist
