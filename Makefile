.PHONY: build
build:
	docker build -t vdhsn/tftp-server .

.PHONY: push
push:
	docker push vdhsn/tftp-server
