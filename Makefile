APP      := replicant
MODULE   := github.com/antiartificial/replicant
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: build run dev clean test lint fmt tidy docker servicectl

build:
	go build -ldflags "$(LDFLAGS)" -o $(APP) ./cmd/replicant/

servicectl:
	go build -ldflags "$(LDFLAGS)" -o servicectl ./cmd/servicectl/

run: build
	./$(APP)

dev:
	go run ./cmd/replicant/

clean:
	rm -f $(APP)
	go clean -cache

test:
	go test ./... -v

lint:
	go vet ./...

fmt:
	gofmt -w -s .

tidy:
	go mod tidy

docker:
	docker build -t $(APP):$(VERSION) -t $(APP):latest .

docker-run: docker
	docker run --rm -it \
		-e ANTHROPIC_API_KEY \
		-e OPENAI_API_KEY \
		-v "$$(pwd)":/work \
		-w /work \
		$(APP):latest
