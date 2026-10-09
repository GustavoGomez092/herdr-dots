.PHONY: build test vet link
build:
	sh scripts/build.sh
test:
	go test -race ./...
vet:
	go vet ./...
link: build
	herdr --session dots-test plugin link .
