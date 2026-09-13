.PHONY: build dashboard-build test lint

build: dashboard-build
	go build -o certhealthz .

# Builds the React dashboard and copies its output into pkg/dashboardui/dist,
# where go:embed picks it up. Required before a release build; go build alone
# works too but serves the placeholder page until this has run once.
dashboard-build:
	cd dashboard && npm ci && npm run build
	rm -rf pkg/dashboardui/dist
	mkdir -p pkg/dashboardui/dist
	cp -r dashboard/dist/. pkg/dashboardui/dist/

test:
	go test ./...

lint:
	go vet ./...
	gofmt -l . | grep -v node_modules || true
