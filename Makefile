.PHONY: configure build run lint format docker plugins
configure:
	python3 scripts/configure.py
build:
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
plugins:
	uv run --project python workflow-plugin validate --root plugins
docker: run
