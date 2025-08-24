BINARY_NAME=go-radio

run:
	bin/$(BINARY_NAME)

build:
	rm -rf ./bin/$(BINARY_NAME) ./static/index.css
	./tailwindcss -i static/base.css -o static/index.css --minify
	go build -o bin/$(BINARY_NAME) .
