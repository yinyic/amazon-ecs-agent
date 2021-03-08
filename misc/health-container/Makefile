.PHONY: all container clean

IMAGE = "amazon/amazon-ecs-health"
TAG = "0.1.0"

all: container

container:
	env GOOS=linux GOARCH=amd64 go build -o health .
	docker build -t "$(IMAGE):$(TAG)" .
	# docker save amazon/amazon-ecs-health:0.1.0 > health-container.tar

clean:
	rm ./health
	# rm health-container.tar
	docker rmi "$(IMAGE):$(TAG)"