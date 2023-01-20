.PHONY: build
build:
	docker build -t vdhsn/tftp-server .

.PHONY: push
push:
	docker push vdhsn/tftp-server

.PHONY: docker-run
docker-run: build
	docker run -p 6969:69/udp -it --rm -v $(PWD)/files:/var/tftp-data:ro --name vdhsn-tftp-server vdhsn/tftp-server

.PHONY: clean
clean:
	docker kill vdhsn-tftp-server

.PHONY: tcpdump
tcpdump:
	tcpdump -XAvvv -i lo udp port 6969

.PHONY: test
test:
	cd /tmp && tftp 127.0.0.1 6969 -c get hello.txt
