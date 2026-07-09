BINARY := bin/zahirdbman
PKG := ./cmd/server
IMAGE := zahirdbman:latest

.PHONY: run build tidy test clean docker-build docker-run up down

run:
	go run $(PKG)

build:
	CGO_ENABLED=0 go build -o $(BINARY) $(PKG)

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -rf bin

docker-build:
	docker build -t $(IMAGE) .

docker-run: docker-build
	docker run --rm -p 8080:8080 --env-file .env $(IMAGE)

up:
	docker compose up --build

down:
	docker compose down
