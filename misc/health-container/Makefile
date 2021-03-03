.PHONY: all container clean

IMAGE = "amazon/amazon-ecs-health"
TAG = "0.1.0"

all: container

container:
	docker build -t "$(IMAGE):$(TAG)" .

clean:
	docker rmi "$(IMAGE):$(TAG)"