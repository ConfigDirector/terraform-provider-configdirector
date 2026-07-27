.PHONY: build

build:
	go build -o bin/terraform-provider-configdirector .

vet:
	go vet ./internal/...

test_integration:
	TF_ACC=1 go test ./internal/... -v
