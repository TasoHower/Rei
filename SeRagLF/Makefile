.PHONY: build test run docker-up docker-down

# ---- Go (gateway) ----

build:
	cd gateway && go build -o bin/gateway ./cmd/gateway/

test:
	cd gateway && go test ./...

run:
	cd gateway && go run ./cmd/gateway/

run-http:
	cd gateway && go run ./cmd/gateway/ -transport http

# ---- Docker ----

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
