EXECUTABLE = build/aws-share-rds-snapshot
ACCOUNT=gianrubio
APP=aws-share-rds-snapshot
BUILD_TAG=0.1
DOCKER_REPO=$(ACCOUNT)/$(APP)
DOCKER_TAG=$(DOCKER_REPO):$(BUILD_TAG)

LDFLAGS ?= -X 'main.Version=$(VERSION)'

ifneq ($(shell uname), Darwin)
	EXTLDFLAGS = -extldflags "-static" $(null)
else
	EXTLDFLAGS =
endif

all: build

build: format
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a --ldflags "$(EXTLDFLAGS)-s -w $(LDFLAGS)" -o $(EXECUTABLE)

format:
	go fmt *.go

image: build
	docker build -t $(DOCKER_TAG) .

push: image
	docker tag $(DOCKER_TAG) $(DOCKER_REPO):latest
	docker push $(DOCKER_TAG)
	docker push $(DOCKER_REPO):latest