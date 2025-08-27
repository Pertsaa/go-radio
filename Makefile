API_BINARY_NAME=api
APP_BINARY_NAME=app

api:
	air --build.cmd "go build -o bin/$(API_BINARY_NAME) cmd/$(API_BINARY_NAME)/main.go" --build.bin "./bin/$(API_BINARY_NAME)" data

app:
	air --build.cmd "./tailwindcss -i internal/static/base.css -o internal/static/index.css --minify && go build -o bin/$(APP_BINARY_NAME) cmd/$(APP_BINARY_NAME)/main.go" --build.bin "./bin/$(APP_BINARY_NAME)"
