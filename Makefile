IMAGE_NAME=kiosk-builder-image
CONTAINER_NAME=kiosk-builder

.PHONY := release
release:
	@docker build -t ${IMAGE_NAME} -f build.dockerfile .
	-@mkdir -p bin
	-@docker rm ${CONTAINER_NAME}
	@docker create --rm --name ${CONTAINER_NAME} ${IMAGE_NAME}
	@docker cp ${CONTAINER_NAME}:/app/bin/kiosk bin/
	-@docker rm ${CONTAINER_NAME}

.PHONY := kiosk
kiosk:
	-@mkdir -p bin
	@CGO_ENABLED=0 go build -o bin/kiosk ./cmd/kiosk/
	-@strip bin/kiosk
	-@ldd bin/kiosk
	-@du -h bin/kiosk

.PHONY := clean
clean:
	-@rm -rf bin
	-@docker rm ${CONTAINER_NAME}
	-@docker rmi ${IMAGE_NAME}

