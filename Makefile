BASE_URL ?= http://localhost:6397
OUT_DIR  ?= lib

.PHONY: generate clean build

generate:
	go run ./cmd/generate -base $(BASE_URL) -out $(OUT_DIR)

clean:
	rm -f $(OUT_DIR)/models.go $(OUT_DIR)/client.go

build: generate
	go build ./$(OUT_DIR)/...
