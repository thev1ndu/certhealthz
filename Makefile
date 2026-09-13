.PHONY: build dashboard-build test e2e lint

build: dashboard-build
	go build -o certhealthz .

# Builds the React dashboard and copies its output into pkg/ui/dist,
# where go:embed picks it up. Required before a release build; go build alone
# works too but serves the placeholder page until this has run once.
dashboard-build:
	cd dashboard && npm ci && npm run build
	rm -rf pkg/ui/dist
	mkdir -p pkg/ui/dist
	cp -r dashboard/dist/. pkg/ui/dist/

test:
	go test ./...

# Runs the end-to-end suite (fake k8s clientsets + local TLS/HTTP servers,
# no real cluster required). Gated behind a build tag so it doesn't slow
# down `make test` / `go test ./...` in everyday dev.
e2e:
	go test -tags=e2e ./e2e/...

lint:
	golangci-lint run
