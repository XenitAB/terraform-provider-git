.ONESHELL:
SHELL := /bin/bash

TEST_COUNT?=1
ACCTEST_PARALLELISM?=5
ACCTEST_TIMEOUT?=10m

all: test testacc build

tidy:
	rm -f go.sum; go mod tidy -compat=1.18

fmt:
	go fmt ./...

vet:
	go vet ./...

test: tidy fmt vet
	go test ./... -coverprofile cover.out

testacc: tidy fmt vet
	TF_ACC=1 go test ./... -v -count $(TEST_COUNT) -parallel $(ACCTEST_PARALLELISM) -timeout $(ACCTEST_TIMEOUT)

build:
	CGO_ENABLED=0 go build -o ./bin/git main.go

install: build
	mkdir -p ~/.terraform.d/plugins/registry.terraform.io/xenitab/git/0.0.0-dev/$${GOOS}_$${GOARCH}
	cp ./bin/git ~/.terraform.d/plugins/registry.terraform.io/xenitab/git/0.0.0-dev/$${GOOS}_$${GOARCH}/terraform-provider-git

.PHONY: docs
docs: tools
	tfplugindocs generate

tools:
	GO111MODULE=on go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

.SILENT:
lint:
	set -e
	echo lint: Start
	EXAMPLES=$$(find examples -mindepth 1 -maxdepth 1 -type d)
	DELETE=examples/data-sources
	echo $${array[@]/$$DELETE}
	EXAMPLES=( "$${EXAMPLES[@]/$$DELETE}" )
	for EXAMPLE in $${EXAMPLES}; do
		echo $$EXAMPLE
		tflint -c examples/.tflint.hcl $${EXAMPLE}
	done
	echo lint: Success
