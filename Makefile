.PHONY: build test run web-dev clean

build:
	pnpm --dir web install --frozen-lockfile
	pnpm --dir web build
	go build -trimpath -o bin/vrac ./cmd/vrac
	go build -trimpath -o bin/vrac-import ./cmd/vrac-import
	go build -trimpath -o bin/vrac-export ./cmd/vrac-export

test:
	go vet ./...
	go test -race ./...
	pnpm --dir web test
	pnpm --dir web build

run:
	go run ./cmd/vrac -web web/dist

web-dev:
	pnpm --dir web dev

clean:
	rm -rf bin web/dist
