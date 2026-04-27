.PHONY: build run spike test fmt vet tidy clean

BIN := bin/sunnytui

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/sunnytui

run: build
	./$(BIN)

# usage: make spike PROMPT="hola, di pong"
spike: build
	./$(BIN) spike "$(PROMPT)"

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
