.PHONY: build demo fmt fmt-check vet test race coverage check clean snapshot live-test live-test-guard

build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/guard9s ./cmd/guard9s

demo: build
	./bin/guard9s --demo

fmt:
	gofmt -w cmd internal

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

coverage:
	go test ./internal/analyzer ./internal/model ./internal/kubernetes ./internal/report -coverprofile=coverage.out
	go tool cover -func=coverage.out
	@go tool cover -func=coverage.out | grep -Eq '^total:.*(9[0-9]\.[0-9]+|100\.0)%'

snapshot:
	GUARD9S_SCREENSHOT="$(CURDIR)/docs/demo.svg" go test ./internal/ui -run TestScreenRenderingAndSnapshot -count=1

live-test:
	bash hack/live-test.sh

live-test-guard:
	bash hack/test-live-test.sh

check: fmt-check vet test race coverage build live-test-guard

clean:
	rm -rf bin dist coverage.out
