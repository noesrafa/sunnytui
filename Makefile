.PHONY: build install run spike test fmt vet tidy clean

BIN := bin/sunnytui

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/sunnytui
	@cp $(BIN) bin/sunny

# Installs both `sunnytui` and the short `sunny` alias to GOBIN (or ~/go/bin).
install:
	go install ./cmd/sunnytui
	@dst="$${GOBIN:-$$HOME/go/bin}"; ln -sf "$$dst/sunnytui" "$$dst/sunny" && echo "linked $$dst/sunny -> sunnytui"

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
