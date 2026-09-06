.PHONY: configure build run lint format test types types-check docker plugins plugins-build plugins-new
configure:
	python3 scripts/configure.py
build: types
	cd web && npm ci && npm run build
	go build -o bin/platform ./cmd/api
run:
	docker compose up -d --build
lint:
	go vet ./...
	cd web && npm run lint
format:
	gofmt -w cmd internal sdk
	cd web && npm run format
# Regenerate the browser's view of the wire contract from the Go structs.
types:
	go run ./cmd/gentypes
types-check:
	go run ./cmd/gentypes -check
test: types-check
	go test ./...
	cd web && npm run test:run
	uv run --project python python -m unittest discover -s python/tests -t python/tests
# Regenerate every manifest from the code that declares it, then seal each package.
plugins-build:
	uv run --project python workflow-plugin build --root plugins
# Verify each manifest matches its declaration and its files, on both runtimes.
plugins:
	uv run --project python workflow-plugin validate --root plugins
	go test ./sdk/go/plugin/ -run TestInstalledPackages
plugins-new:
	@test -n "$(NAME)" || (echo 'usage: make plugins-new NAME=team.my-plugin' && exit 1)
	uv run --project python workflow-plugin new $(NAME) --root plugins
docker: run
