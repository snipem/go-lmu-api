BASE_URL ?= http://localhost:6397
OUT_DIR  ?= lib

.PHONY: generate clean build standings

generate:
	go run ./cmd/generate -base $(BASE_URL) -out $(OUT_DIR)

clean:
	rm -f $(OUT_DIR)/models.go $(OUT_DIR)/client.go standings.exe

build: generate
	go build ./$(OUT_DIR)/...

standings:
	go build -o standings.exe ./cmd/standings
