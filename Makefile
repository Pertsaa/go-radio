RADIO_BINARY_NAME=radio
APP_BINARY_NAME=app
DISCORD_BINARY_NAME=discord
DISCORD_TOKEN=

radio:
	air --build.cmd "go build -o bin/$(RADIO_BINARY_NAME) cmd/$(RADIO_BINARY_NAME)/main.go" --build.bin "./bin/$(RADIO_BINARY_NAME)" data

app:
	air --build.cmd "./tailwindcss -i internal/static/base.css -o internal/static/index.css --minify && go build -o bin/$(APP_BINARY_NAME) cmd/$(APP_BINARY_NAME)/main.go" --build.bin "./bin/$(APP_BINARY_NAME)"

discord:
	air --build.cmd "go build -o bin/$(DISCORD_BINARY_NAME) cmd/$(DISCORD_BINARY_NAME)/main.go" --build.bin "./bin/$(DISCORD_BINARY_NAME)" -- -t $(DISCORD_TOKEN)

formatter:
	go build -o bin/formatter cmd/formatter/main.go
